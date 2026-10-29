package hugo

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"
)

func TestLatestHugoVersion(t *testing.T) {
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		if got := r.Header.Get("User-Agent"); got == "" {
			t.Errorf("missing user agent")
		}
		_, _ = w.Write([]byte(`{"tag_name":"v0.169.0","name":"0.169.0"}`))
	}))
	defer srv.Close()
	t.Setenv("HUGO_LATEST_URL", srv.URL)

	svc := NewService()
	if got := svc.LatestHugoVersion(t.Context()); got != "0.169.0" {
		t.Fatalf("latest = %q", got)
	}
	// The result is cached, so a second call does not hit the API again.
	if got := svc.LatestHugoVersion(t.Context()); got != "0.169.0" {
		t.Fatalf("cached latest = %q", got)
	}
	if hits.Load() != 1 {
		t.Fatalf("api hits = %d, want 1", hits.Load())
	}
}

func TestLatestHugoVersionUnavailable(t *testing.T) {
	t.Setenv("HUGO_LATEST_URL", "http://127.0.0.1:1")
	svc := NewService()
	if got := svc.LatestHugoVersion(t.Context()); got != "" {
		t.Fatalf("latest = %q, want empty when the check fails", got)
	}
}

func TestLatestHugoVersionDisabled(t *testing.T) {
	t.Setenv("HUGO_LATEST_URL", "")
	svc := NewService()
	if got := svc.LatestHugoVersion(t.Context()); got != "" {
		t.Fatalf("latest = %q, want empty when the check is disabled", got)
	}
}

func TestLatestHugoVersionRejectsBadPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"not-a-version"}`))
	}))
	defer srv.Close()
	t.Setenv("HUGO_LATEST_URL", srv.URL)
	if got := NewService().LatestHugoVersion(t.Context()); got != "" {
		t.Fatalf("latest = %q, want empty for an unparsable tag", got)
	}
}
