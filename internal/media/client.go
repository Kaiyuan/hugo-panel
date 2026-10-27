package media

import (
	"context"
	"crypto/tls"
	"fmt"
	"net"
	"net/http"
	"strings"
	"time"
)

// storageClient builds an HTTP client for S3/WebDAV. Three per-library escape
// hatches are supported:
//
//   - insecure disables TLS verification for self-signed NAS certificates.
//   - hostIP dials a fixed IP while keeping the URL hostname for the Host
//     header and TLS SNI. This works around Docker DNS that cannot resolve the
//     NAS name, without having to rebuild the container with extra_hosts.
//   - dohURL resolves the URL hostname through DNS over HTTPS (port 443) when
//     the container cannot reach any DNS server on port 53. hostIP takes
//     precedence when both are set.
func storageClient(insecure bool, hostname, hostIP, dohURL string) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit admin opt-in
	}
	dialer := &net.Dialer{Timeout: 30 * time.Second, KeepAlive: 30 * time.Second}
	if hostIP != "" && hostname != "" {
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
	} else if dohURL != "" && hostname != "" {
		if resolver, err := dohResolverFor(dohURL); err == nil {
			// Resolve the destination ourselves instead of leaving it to the
			// container's broken resolver. Proxying would bypass DialContext
			// and reintroduce that lookup, so it is disabled here too.
			transport.Proxy = nil
			transport.DialContext = func(ctx context.Context, network, addr string) (net.Conn, error) {
				host, port, err := net.SplitHostPort(addr)
				if err != nil || !hostMatches(host, hostname) {
					return dialer.DialContext(ctx, network, addr)
				}
				if net.ParseIP(strings.Trim(host, "[]")) != nil {
					return dialer.DialContext(ctx, network, addr)
				}
				addrs, err := resolver.Resolve(ctx, host)
				if err != nil {
					return nil, err
				}
				var lastErr error
				for _, ip := range addrs {
					conn, dialErr := dialer.DialContext(ctx, network, net.JoinHostPort(ip, port))
					if dialErr == nil {
						return conn, nil
					}
					lastErr = dialErr
				}
				return nil, fmt.Errorf("lookup %s via DNS over HTTPS: %w", host, lastErr)
			}
		}
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: transport}
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
		return errf("storage_unreachable", "DNS over HTTPS lookup failed inside the panel container: %v. Check that the DoH URL is reachable from the container (https://1.1.1.1/dns-query is a good default), or set the server IP in the “Host IP” field.", err)
	}
	if strings.Contains(lower, "lookup") && (strings.Contains(lower, "timeout") || strings.Contains(lower, "server misbehaving") || strings.Contains(lower, "no such host") || strings.Contains(lower, "temporary failure")) {
		return errf("storage_unreachable", "DNS lookup failed inside the panel container: %v. Enter the server IP in the storage’s “Host IP” field, or set a DNS over HTTPS URL, or add the host to Docker with --add-host / extra_hosts.", err)
	}
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls:") {
		return errf("storage_unreachable", "%s TLS certificate could not be verified: %v. Enable “Allow self-signed certificates” for this storage if the server uses a private certificate.", service, err)
	}
	return errf("storage_unreachable", "Could not reach the %s server: %v", service, err)
}
