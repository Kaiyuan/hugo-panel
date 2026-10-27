package media

import (
	"bytes"
	"context"
	"crypto/rand"
	"encoding/binary"
	"fmt"
	"io"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// dnsResolver resolves hostnames through a DNS over HTTPS endpoint. It exists
// for containers whose embedded DNS cannot reach the outside world: DoH uses
// HTTPS (port 443), which the same network usually allows, and it keeps working
// when outbound port 53 is blocked.
//
// The endpoint should normally be an IP-based URL such as
// https://1.1.1.1/dns-query so resolving the DoH server itself does not need
// the resolver that is broken.
type dnsResolver struct {
	url    string
	client *http.Client
	now    func() time.Time

	mu    sync.Mutex
	cache map[string]dnsCacheEntry
}

type dnsCacheEntry struct {
	addrs   []string
	expires time.Time
}

const (
	dohQueryTimeout = 10 * time.Second
	dohMinTTL       = 10 * time.Second
	dohMaxTTL       = 10 * time.Minute
	dohMaxCNAMEHops = 8
)

const (
	dnsTypeA     uint16 = 1
	dnsTypeCNAME uint16 = 5
	dnsTypeAAAA  uint16 = 28
	dnsClassINET uint16 = 1
)

// dohResolverFor is a seam for tests that need to supply a resolver with a
// custom HTTP client (for example against an httptest TLS server).
var dohResolverFor = sharedDOHResolver

var (
	dohResolversMu sync.Mutex
	dohResolvers   = make(map[string]*dnsResolver)
)

// sharedDOHResolver keeps one resolver per endpoint for the life of the
// process. Backends are rebuilt for every media request, so without this the
// TTL cache would be useless and browsing a folder would trigger a DoH lookup
// for each preview.
func sharedDOHResolver(rawURL string) (*dnsResolver, error) {
	dohResolversMu.Lock()
	defer dohResolversMu.Unlock()
	if resolver, ok := dohResolvers[rawURL]; ok {
		return resolver, nil
	}
	resolver, err := newDOHResolver(rawURL)
	if err != nil {
		return nil, err
	}
	dohResolvers[rawURL] = resolver
	return resolver, nil
}

func newDOHResolver(rawURL string) (*dnsResolver, error) {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" {
		return nil, errf("invalid_request", "DNS over HTTPS URL is required")
	}
	req, err := http.NewRequest(http.MethodPost, rawURL, nil)
	if err != nil || req.URL.Scheme != "https" || req.URL.Host == "" {
		return nil, errf("invalid_request", "DNS over HTTPS URL must be an absolute https URL")
	}
	return &dnsResolver{
		url:    rawURL,
		client: &http.Client{Timeout: dohQueryTimeout},
		now:    time.Now,
		cache:  make(map[string]dnsCacheEntry),
	}, nil
}

// Resolve looks up host, preferring IPv4 addresses (IPv6-only results are used
// only when no A record exists) so a server with a broken IPv6 route still
// works. Results are cached for the smallest TTL in the answer.
func (r *dnsResolver) Resolve(ctx context.Context, host string) ([]string, error) {
	key := strings.ToLower(strings.TrimSuffix(strings.TrimSpace(host), "."))
	if key == "" {
		return nil, errf("storage_unreachable", "DNS over HTTPS cannot resolve an empty hostname")
	}
	if net.ParseIP(key) != nil {
		return []string{key}, nil
	}
	now := r.now()
	r.mu.Lock()
	entry, ok := r.cache[key]
	r.mu.Unlock()
	if ok && now.Before(entry.expires) {
		return append([]string(nil), entry.addrs...), nil
	}

	addrs, ttl, err := r.lookup(ctx, key, dnsTypeA)
	if err != nil {
		return nil, err
	}
	if len(addrs) == 0 {
		addrs, ttl, err = r.lookup(ctx, key, dnsTypeAAAA)
		if err != nil {
			return nil, err
		}
	}
	if len(addrs) == 0 {
		return nil, fmt.Errorf("lookup %s via DNS over HTTPS: no A or AAAA records", key)
	}
	r.mu.Lock()
	r.cache[key] = dnsCacheEntry{addrs: append([]string(nil), addrs...), expires: now.Add(ttl)}
	r.mu.Unlock()
	return addrs, nil
}

func (r *dnsResolver) lookup(ctx context.Context, host string, qtype uint16) ([]string, time.Duration, error) {
	query, id, err := buildDNSQuery(host, qtype)
	if err != nil {
		return nil, 0, err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, r.url, bytes.NewReader(query))
	if err != nil {
		return nil, 0, fmt.Errorf("lookup %s via DNS over HTTPS: %w", host, err)
	}
	req.Header.Set("Content-Type", "application/dns-message")
	req.Header.Set("Accept", "application/dns-message")

	resp, err := r.client.Do(req)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup %s via DNS over HTTPS (%s): %w", host, r.url, err)
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 64<<10))
	if err != nil {
		return nil, 0, fmt.Errorf("lookup %s via DNS over HTTPS: %w", host, err)
	}
	if resp.StatusCode != http.StatusOK {
		return nil, 0, fmt.Errorf("lookup %s via DNS over HTTPS: %s returned HTTP %d", host, r.url, resp.StatusCode)
	}
	addrs, ttl, err := parseDNSResponse(body, id, host, qtype)
	if err != nil {
		return nil, 0, fmt.Errorf("lookup %s via DNS over HTTPS: %w", host, err)
	}
	return addrs, ttl, nil
}

func buildDNSQuery(host string, qtype uint16) ([]byte, uint16, error) {
	var idBytes [2]byte
	if _, err := rand.Read(idBytes[:]); err != nil {
		return nil, 0, fmt.Errorf("could not create a DNS query id: %w", err)
	}
	id := binary.BigEndian.Uint16(idBytes[:])
	buf := make([]byte, 12, 12+len(host)+6)
	binary.BigEndian.PutUint16(buf[0:2], id)
	binary.BigEndian.PutUint16(buf[2:4], 0x0100) // recursion desired
	binary.BigEndian.PutUint16(buf[4:6], 1)      // one question
	name := strings.TrimSuffix(host, ".")
	if name == "" || len(name) > 253 {
		return nil, 0, fmt.Errorf("invalid hostname %q", host)
	}
	for _, label := range strings.Split(name, ".") {
		if label == "" || len(label) > 63 {
			return nil, 0, fmt.Errorf("invalid hostname %q", host)
		}
		buf = append(buf, byte(len(label)))
		buf = append(buf, label...)
	}
	buf = append(buf, 0)
	buf = binary.BigEndian.AppendUint16(buf, qtype)
	buf = binary.BigEndian.AppendUint16(buf, dnsClassINET)
	return buf, id, nil
}

// parseDNSResponse extracts A/AAAA addresses for qname, following CNAME chains
// inside the answer section. DNS names may use compression pointers, so every
// name is decoded relative to the whole message.
func parseDNSResponse(msg []byte, id uint16, qname string, qtype uint16) ([]string, time.Duration, error) {
	if len(msg) < 12 {
		return nil, 0, fmt.Errorf("short DNS response")
	}
	if got := binary.BigEndian.Uint16(msg[0:2]); got != id {
		return nil, 0, fmt.Errorf("DNS response id mismatch")
	}
	flags := binary.BigEndian.Uint16(msg[2:4])
	if rcode := flags & 0x000f; rcode != 0 {
		return nil, 0, fmt.Errorf("DNS server returned error code %d", rcode)
	}
	qd := int(binary.BigEndian.Uint16(msg[4:6]))
	an := int(binary.BigEndian.Uint16(msg[6:8]))

	off := 12
	questionName := ""
	for i := 0; i < qd; i++ {
		name, next, err := decodeDNSName(msg, off)
		if err != nil {
			return nil, 0, err
		}
		if i == 0 {
			questionName = name
		}
		off = next + 4 // qtype + qclass
		if off > len(msg) {
			return nil, 0, fmt.Errorf("truncated DNS question")
		}
	}
	if questionName == "" {
		questionName = qname
	}

	type record struct {
		name   string
		qtype  uint16
		ttl    uint32
		addr   string
		target string
	}
	records := make([]record, 0, an)
	ttl := uint32(0)
	for i := 0; i < an; i++ {
		name, next, err := decodeDNSName(msg, off)
		if err != nil {
			return nil, 0, err
		}
		off = next
		if off+10 > len(msg) {
			return nil, 0, fmt.Errorf("truncated DNS answer")
		}
		rtype := binary.BigEndian.Uint16(msg[off : off+2])
		class := binary.BigEndian.Uint16(msg[off+2 : off+4])
		rttl := binary.BigEndian.Uint32(msg[off+4 : off+8])
		rdlen := int(binary.BigEndian.Uint16(msg[off+8 : off+10]))
		off += 10
		if off+rdlen > len(msg) {
			return nil, 0, fmt.Errorf("truncated DNS answer data")
		}
		rec := record{name: strings.ToLower(strings.TrimSuffix(name, ".")), qtype: rtype, ttl: rttl}
		if class == dnsClassINET {
			switch rtype {
			case dnsTypeA:
				if rdlen == net.IPv4len {
					rec.addr = net.IP(msg[off : off+rdlen]).String()
				}
			case dnsTypeAAAA:
				if rdlen == net.IPv6len {
					rec.addr = net.IP(msg[off : off+rdlen]).String()
				}
			case dnsTypeCNAME:
				target, _, err := decodeDNSName(msg, off)
				if err != nil {
					return nil, 0, err
				}
				rec.target = strings.ToLower(strings.TrimSuffix(target, "."))
			}
		}
		if rttl < ttl || ttl == 0 {
			ttl = rttl
		}
		records = append(records, rec)
		off += rdlen
	}

	cnames := make(map[string]string)
	for _, rec := range records {
		if rec.qtype == dnsTypeCNAME && rec.target != "" {
			cnames[rec.name] = rec.target
		}
	}
	// Follow the chain from the queried name (or the question name when the
	// server echoes a slightly different spelling).
	current := strings.ToLower(strings.TrimSuffix(questionName, "."))
	if current == "" {
		current = strings.ToLower(strings.TrimSuffix(qname, "."))
	}
	seen := make(map[string]bool)
	for hop := 0; hop <= dohMaxCNAMEHops; hop++ {
		if seen[current] {
			return nil, 0, fmt.Errorf("DNS CNAME loop at %s", current)
		}
		seen[current] = true
		var addrs []string
		for _, rec := range records {
			if rec.name == current && rec.qtype == qtype && rec.addr != "" {
				addrs = append(addrs, rec.addr)
			}
		}
		if len(addrs) > 0 {
			return addrs, clampTTL(ttl), nil
		}
		next, ok := cnames[current]
		if !ok {
			break
		}
		current = next
	}
	return nil, clampTTL(ttl), nil
}

func clampTTL(ttl uint32) time.Duration {
	d := time.Duration(ttl) * time.Second
	if d < dohMinTTL {
		return dohMinTTL
	}
	if d > dohMaxTTL {
		return dohMaxTTL
	}
	return d
}

func decodeDNSName(msg []byte, off int) (string, int, error) {
	var labels []string
	next := -1
	jumps := 0
	for {
		if off < 0 || off >= len(msg) {
			return "", 0, fmt.Errorf("truncated DNS name")
		}
		length := int(msg[off])
		if length == 0 {
			if next < 0 {
				next = off + 1
			}
			break
		}
		switch length & 0xc0 {
		case 0xc0:
			if off+1 >= len(msg) {
				return "", 0, fmt.Errorf("truncated DNS compression pointer")
			}
			ptr := int(binary.BigEndian.Uint16(msg[off:off+2]) & 0x3fff)
			if next < 0 {
				next = off + 2
			}
			jumps++
			if jumps > 32 {
				return "", 0, fmt.Errorf("DNS compression pointer loop")
			}
			off = ptr
			continue
		case 0x00:
			off++
			if off+length > len(msg) {
				return "", 0, fmt.Errorf("truncated DNS label")
			}
			labels = append(labels, string(msg[off:off+length]))
			off += length
		default:
			return "", 0, fmt.Errorf("unsupported DNS label type")
		}
	}
	return strings.Join(labels, "."), next, nil
}
