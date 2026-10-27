package media

import (
	"context"
	"encoding/binary"
	"io"
	"net"
	"net/http"
	"net/http/httptest"
	"net/url"
	"sync/atomic"
	"testing"
	"time"
)

type testDNSAnswer struct {
	name string
	typ  uint16
	ttl  uint32
	data []byte
}

func encodeTestName(name string) []byte {
	var out []byte
	for _, label := range splitTestName(name) {
		out = append(out, byte(len(label)))
		out = append(out, label...)
	}
	return append(out, 0)
}

func splitTestName(name string) []string {
	var parts []string
	start := 0
	for i := 0; i < len(name); i++ {
		if name[i] == '.' {
			parts = append(parts, name[start:i])
			start = i + 1
		}
	}
	if start < len(name) {
		parts = append(parts, name[start:])
	}
	return parts
}

func buildDNSReply(id uint16, qname string, qtype uint16, rcode uint16, answers []testDNSAnswer) []byte {
	buf := make([]byte, 12)
	binary.BigEndian.PutUint16(buf[0:2], id)
	binary.BigEndian.PutUint16(buf[2:4], 0x8180|rcode&0xf)
	binary.BigEndian.PutUint16(buf[4:6], 1)
	binary.BigEndian.PutUint16(buf[6:8], uint16(len(answers)))
	buf = append(buf, encodeTestName(qname)...)
	buf = binary.BigEndian.AppendUint16(buf, qtype)
	buf = binary.BigEndian.AppendUint16(buf, dnsClassINET)
	for _, ans := range answers {
		buf = append(buf, encodeTestName(ans.name)...)
		buf = binary.BigEndian.AppendUint16(buf, ans.typ)
		buf = binary.BigEndian.AppendUint16(buf, dnsClassINET)
		buf = binary.BigEndian.AppendUint32(buf, ans.ttl)
		buf = binary.BigEndian.AppendUint16(buf, uint16(len(ans.data)))
		buf = append(buf, ans.data...)
	}
	return buf
}

func testDOHServer(t *testing.T, respond func(qname string, qtype uint16) []testDNSAnswer) (*httptest.Server, *dnsResolver) {
	t.Helper()
	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			http.Error(w, "method", http.StatusMethodNotAllowed)
			return
		}
		if ct := r.Header.Get("Content-Type"); ct != "application/dns-message" {
			http.Error(w, "content type", http.StatusUnsupportedMediaType)
			return
		}
		body, err := io.ReadAll(r.Body)
		if err != nil || len(body) < 12 {
			http.Error(w, "bad query", http.StatusBadRequest)
			return
		}
		id := binary.BigEndian.Uint16(body[0:2])
		qtype := binary.BigEndian.Uint16(body[len(body)-4 : len(body)-2])
		qname, _, err := decodeDNSName(body, 12)
		if err != nil {
			http.Error(w, "bad name", http.StatusBadRequest)
			return
		}
		reply := buildDNSReply(id, qname, qtype, 0, respond(qname, qtype))
		w.Header().Set("Content-Type", "application/dns-message")
		_, _ = w.Write(reply)
	}))
	t.Cleanup(srv.Close)
	resolver, err := newDOHResolver(srv.URL + "/dns-query")
	if err != nil {
		t.Fatal(err)
	}
	resolver.client = srv.Client()
	resolver.client.Timeout = 5 * time.Second
	return srv, resolver
}

func TestDOHResolverResolvesA(t *testing.T) {
	_, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		if qtype != dnsTypeA {
			return nil
		}
		return []testDNSAnswer{{name: qname, typ: dnsTypeA, ttl: 300, data: net.IPv4(203, 0, 113, 9).To4()}}
	})
	addrs, err := resolver.Resolve(context.Background(), "dav.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != "203.0.113.9" {
		t.Fatalf("addrs = %v", addrs)
	}
}

func TestDOHResolverFollowsCNAME(t *testing.T) {
	_, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		if qtype != dnsTypeA {
			return nil
		}
		return []testDNSAnswer{
			{name: qname, typ: dnsTypeCNAME, ttl: 300, data: encodeTestName("edge.example.test")},
			{name: "edge.example.test", typ: dnsTypeA, ttl: 120, data: net.IPv4(198, 51, 100, 4).To4()},
		}
	})
	addrs, err := resolver.Resolve(context.Background(), "dav.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != "198.51.100.4" {
		t.Fatalf("addrs = %v", addrs)
	}
}

func TestDOHResolverFallsBackToAAAA(t *testing.T) {
	_, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		if qtype != dnsTypeAAAA {
			return nil
		}
		return []testDNSAnswer{{name: qname, typ: dnsTypeAAAA, ttl: 300, data: net.ParseIP("2001:db8::42").To16()}}
	})
	addrs, err := resolver.Resolve(context.Background(), "v6.example.test")
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != "2001:db8::42" {
		t.Fatalf("addrs = %v", addrs)
	}
}

func TestDOHResolverCachesAnswers(t *testing.T) {
	var calls atomic.Int32
	_, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		calls.Add(1)
		return []testDNSAnswer{{name: qname, typ: dnsTypeA, ttl: 300, data: net.IPv4(203, 0, 113, 9).To4()}}
	})
	for i := 0; i < 3; i++ {
		if _, err := resolver.Resolve(context.Background(), "cache.example.test"); err != nil {
			t.Fatal(err)
		}
	}
	if got := calls.Load(); got != 1 {
		t.Fatalf("DoH requests = %d, want 1 (cached)", got)
	}
	// Once the cached entry expires the resolver must query again.
	resolver.now = func() time.Time { return time.Now().Add(11 * time.Minute) }
	if _, err := resolver.Resolve(context.Background(), "cache.example.test"); err != nil {
		t.Fatal(err)
	}
	if got := calls.Load(); got != 2 {
		t.Fatalf("DoH requests = %d, want 2 after expiry", got)
	}
}

func TestDOHResolverReportsServerErrors(t *testing.T) {
	_, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		return nil
	})
	if _, err := resolver.Resolve(context.Background(), "missing.example.test"); err == nil {
		t.Fatal("expected an error when there are no records")
	}

	srv := httptest.NewTLSServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "unavailable", http.StatusServiceUnavailable)
	}))
	defer srv.Close()
	failing, err := newDOHResolver(srv.URL + "/dns-query")
	if err != nil {
		t.Fatal(err)
	}
	failing.client = srv.Client()
	if _, err := failing.Resolve(context.Background(), "dav.example.test"); err == nil {
		t.Fatal("expected an HTTP 503 error")
	}
}

func TestDOHParserHandlesNXDOMAINAndCompression(t *testing.T) {
	query, id, err := buildDNSQuery("dav.example.test", dnsTypeA)
	if err != nil {
		t.Fatal(err)
	}
	nx := buildDNSReply(id, "dav.example.test", dnsTypeA, 3, nil)
	if _, _, err := parseDNSResponse(nx, id, "dav.example.test", dnsTypeA); err == nil {
		t.Fatal("expected NXDOMAIN to be reported")
	}

	// Build a response whose answer name is a compression pointer to the
	// question name at offset 12, as real DNS servers do.
	msg := append([]byte(nil), query...)
	binary.BigEndian.PutUint16(msg[2:4], 0x8180)
	binary.BigEndian.PutUint16(msg[6:8], 1)
	msg = append(msg, 0xc0, 0x0c) // name -> offset 12
	msg = binary.BigEndian.AppendUint16(msg, dnsTypeA)
	msg = binary.BigEndian.AppendUint16(msg, dnsClassINET)
	msg = binary.BigEndian.AppendUint32(msg, 60)
	msg = binary.BigEndian.AppendUint16(msg, 4)
	msg = append(msg, 192, 0, 2, 7)
	addrs, ttl, err := parseDNSResponse(msg, id, "dav.example.test", dnsTypeA)
	if err != nil {
		t.Fatal(err)
	}
	if len(addrs) != 1 || addrs[0] != "192.0.2.7" || ttl != 60*time.Second {
		t.Fatalf("addrs = %v ttl = %v", addrs, ttl)
	}
}

func TestSharedDOHResolverReusesEndpoint(t *testing.T) {
	first, err := sharedDOHResolver("https://1.1.1.1/dns-query")
	if err != nil {
		t.Fatal(err)
	}
	second, err := sharedDOHResolver("https://1.1.1.1/dns-query")
	if err != nil {
		t.Fatal(err)
	}
	if first != second {
		t.Fatal("expected the resolver for one endpoint to be reused")
	}
	if _, err := sharedDOHResolver("http://1.1.1.1/dns-query"); err == nil {
		t.Fatal("expected plain http DoH endpoints to be rejected")
	}
}

func TestStorageClientDOHBypassesSystemDNS(t *testing.T) {
	srv, resolver := testDOHServer(t, func(qname string, qtype uint16) []testDNSAnswer {
		if qtype != dnsTypeA {
			return nil
		}
		return []testDNSAnswer{{name: qname, typ: dnsTypeA, ttl: 300, data: net.IPv4(127, 0, 0, 1).To4()}}
	})
	var seenHost string
	target := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		seenHost = r.Host
		w.WriteHeader(http.StatusMultiStatus)
	}))
	defer target.Close()
	targetURL, _ := url.Parse(target.URL)

	previous := dohResolverFor
	dohResolverFor = func(rawURL string) (*dnsResolver, error) {
		resolver.url = rawURL
		return resolver, nil
	}
	defer func() { dohResolverFor = previous }()

	cfg := Config{
		ID: "dav", Name: "dav", Type: "webdav",
		BaseURL: "http://nas.invalid:" + targetURL.Port() + "/dav",
		DOHURL:  srv.URL + "/dns-query",
	}
	b, err := newWebDAV(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := b.Test(context.Background()); err != nil {
		t.Fatalf("DoH override test: %v", err)
	}
	if seenHost != "nas.invalid:"+targetURL.Port() {
		t.Fatalf("Host header = %q, want the original hostname", seenHost)
	}
}
