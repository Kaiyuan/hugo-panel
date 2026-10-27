package media

import (
	"crypto/tls"
	"net/http"
	"strings"
	"time"
)

// storageClient builds an HTTP client for S3/WebDAV. Some NAS and self-hosted
// servers use self-signed certificates; that is an explicit per-library opt-in.
func storageClient(insecure bool) *http.Client {
	transport := http.DefaultTransport.(*http.Transport).Clone()
	transport.Proxy = http.ProxyFromEnvironment
	if insecure {
		transport.TLSClientConfig = &tls.Config{InsecureSkipVerify: true} //nolint:gosec // explicit admin opt-in
	}
	return &http.Client{Timeout: 10 * time.Minute, Transport: transport}
}

func networkError(err error, service string) error {
	msg := err.Error()
	if strings.Contains(msg, "x509") || strings.Contains(msg, "certificate") || strings.Contains(msg, "tls:") {
		return errf("storage_unreachable", "%s TLS certificate could not be verified: %v. Enable “Allow self-signed certificates” for this storage if the server uses a private certificate.", service, err)
	}
	return errf("storage_unreachable", "Could not reach the %s server: %v", service, err)
}
