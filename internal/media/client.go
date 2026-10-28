package media

import (
	"context"
	"crypto/tls"
	"errors"
	"fmt"
	"net"
	"net/http"
	"strings"
	"sync"
	"time"
)

// storageClient builds an HTTP client for S3/WebDAV. The escape hatches are:
//
//   - insecure disables TLS verification for self-signed NAS certificates.
//   - hostIP dials a fixed IP while keeping the URL hostname for the Host
//     header and TLS SNI. This works around Docker DNS that cannot resolve the
//     NAS name, without having to rebuild the container with extra_hosts.
//   - dohURL resolves the URL hostname through DNS over HTTPS (port 443) when
//     the container cannot reach any DNS server on port 53. hostIP takes
//     precedence when both are set.
//
// Without an explicit setting, a failed system DNS lookup falls back to public
// DoH resolvers automatically. That keeps Docker setups usable out of the box
// when outbound port 53 is blocked. Private-looking names (single-label,
// .local, .lan, ...) are left alone because public resolvers cannot answer
// them.
func storageClient(insecure bool, hostname, hostIP, dohURL string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit admin opt-in
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	switch {
	case hostIP != "" && hostname != "":
		// A fixed IP is only meaningful for a direct connection. Sending the
		// request through HTTP_PROXY would let the proxy resolve the hostname
		// again (or fail), so disable proxying in this mode.
		transport.Proxy = nil
		transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
			host, port, err := net.SplitHostPort(addr)
			if err == nil && hostMatches(host, hostname) {
				addr = net.JoinHostPort(hostIP, port)
			}
			return dialer.DialContext(ctx, network, addr)
		}
	case dohURL != "" && hostname != "":
		if resolver, err := dohResolverFor(dohURL); err == nil {
			// Resolve the destination ourselves instead of leaving it to the
			// container's broken resolver. Proxying would bypass DialContext
			// and reintroduce that lookup, so it is disabled here too.
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil || !hostMatches(host, hostname) || net.ParseIP(strings.Trim(host, "[]")) != nil {
					return dialer.DialContext(ctx, network, addr)
				}
				addrs, err := resolver.Resolve(ctx, host)
				if err != nil {
					return nil, err
				}
				return dialResolved(ctx, dialer, network, host, port, addrs)
			}
		}
	case hostname != "":
		transport.DialContext = automaticDOHDial(dialer, hostname)
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: transport}
}

// automaticDOHDial first uses the system resolver and retries through DoH when
// it fails. A host that recently failed is sent straight to DoH to avoid
// paying the system timeout again on every preview request.
func automaticDOHDial(dialer *net.Dialer, hostname string) func(context.Context, string, string) (net.Conn, error) {
	return func(ctx context.Context, network, addr string) (net.Conn, error) {
		host, port, err := net.SplitHostPort(addr)
		if err != nil || !hostMatches(host, hostname) || net.ParseIP(strings.Trim(host, "[]")) != nil || localHostname(host) {
			return dialer.DialContext(ctx, network, addr)
		}
		if dnsRecentlyFailed(host) {
			addrs, err := resolveWithFallback(ctx, host)
			if err != nil {
				return nil, err
			}
			return dialResolved(ctx, dialer, network, host, port, addrs)
		}
		conn, err := dialer.DialContext(ctx, network, addr)
		if err == nil {
			return conn, nil
		}
		var dnsErr *net.DNSError
		if !errors.As(err, &dnsErr) {
			return nil, err
		}
		addrs, dohErr := resolveWithFallback(ctx, host)
		if dohErr != nil {
			return nil, fmt.Errorf("system DNS failed (%v), and DNS over HTTPS fallback failed: %w", err, dohErr)
		}
		markDNSFailure(host)
		return dialResolved(ctx, dialer, network, host, port, addrs)
	}
}

func dialResolved(ctx context.Context, dialer *net.Dialer, network, host, port string, addrs []string) (net.Conn, error) {
	var lastErr error
	for _, ip := range addrs {
		conn, err := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
		if err == nil {
			return conn, nil
		}
		lastErr = err
	}
	if lastErr == nil {
		lastErr = fmt.Errorf("no addresses returned")
	}
	return nil, fmt.Errorf("lookup %s via DNS over HTTPS: %w", host, lastErr)
}

const dnsFailureTTL = 10 * time.Minute

var (
	dnsFailureMu sync.Mutex
	dnsFailures  = make(map[string]time.Time)
)

func markDNSFailure(host string) {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	dnsFailureMu.Lock()
	defer dnsFailureMu.Unlock()
	dnsFailures[host] = time.Now().Add(dnsFailureTTL)
}

func dnsRecentlyFailed(host string) bool {
	host = strings.ToLower(strings.TrimSuffix(host, "."))
	dnsFailureMu.Lock()
	defer dnsFailureMu.Unlock()
	expires, ok := dnsFailures[host]
	if !ok {
		return false
	}
	if time.Now().After(expires) {
		delete(dnsFailures, host)
		return false
	}
	return true
}

// localHostname reports names that a public DoH resolver cannot resolve. They
// are left to the system resolver (Docker service names, mDNS names, ...).
func localHostname(host string) bool {
	h := strings.TrimSuffix(strings.ToLower(host), ".")
	if !strings.Contains(h, ".") {
		return true
	}
	for _, suffix := range []string{".local", ".localhost", ".localdomain", ".lan", ".home", ".internal", ".arpa"} {
		if strings.HasSuffix(h, suffix) {
			return true
		}
	}
	return false
}

// hostMatches accepts the configured hostname and its subdomains, because S3
// virtual-host addressing uses bucket.endpoint.example.com.
func hostMatches(host, hostname string) bool {
	host = strings.TrimSuffix(strings.ToLower(host), ".")
	hostname = strings.TrimSuffix(strings.ToLower(hostname), ".")
	return host == hostname || strings.HasSuffix(host, "."+hostname)
}

func networkError(err error, service string) error {
	msg := err.Error()
	lower := strings.ToLower(msg)
	if strings.Contains(lower, "dns over https") {
		return errf("storage_unreachable", "DNS resolution failed inside the panel container, including the DNS over HTTPS fallback: %v. Check that the container can reach a DoH endpoint over HTTPS (1.1.1.1 is a common default), or enter the server IP in the storage’s “Host IP” field.", err)
	}
	if strings.Contains(lower, "lookup") && (strings.Contains(lower, "timeout") || strings.Contains(lower, "server misbehaving") || strings.Contains(lower, "no such host") || strings.Contains(lower, "temporary failure")) {
		return errf("storage_unreachable", "DNS lookup failed inside the panel container: %v. Enter the server IP in the storage’s “Host IP” field, configure a DNS over HTTPS URL, or add the host to Docker with --add-host / extra_hosts.", err)
	}
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls:") {
		return errf("storage_unreachable", "%s TLS certificate could not be verified: %v. Enable “Allow self-signed certificates” for this storage if the server uses a private certificate.", service, err)
	}
	return errf("storage_unreachable", "Could not reach the %s server: %v", service, err)
}
