package release

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestNormalize(t *testing.T) {
	cases := map[string]string{
		"v0.0.5":        "0.0.5",
		"0.0.10":        "0.0.10",
		"release-1.2.3": "1.2.3",
	}
	for input, want := range cases {
		got, ok := Normalize(input)
		if !ok || got != want {
			t.Errorf("Normalize(%q) = %q, %v; want %q", input, got, ok, want)
		}
	}
	if _, ok := Normalize("latest"); ok {
		t.Error("Normalize accepted a non-version tag")
	}
}

func TestIsNewer(t *testing.T) {
	if !IsNewer("0.0.6", "0.0.5") || !IsNewer("0.1.0", "0.0.9") || !IsNewer("1.0.0", "0.9.9") {
		t.Error("newer versions were not detected")
	}
	if IsNewer("0.0.5", "0.0.5") || IsNewer("0.0.4", "0.0.5") {
		t.Error("same or older versions were treated as newer")
	}
	if IsNewer("", "0.0.5") || IsNewer("0.0.6", "dev") {
		t.Error("unparsable versions should not be treated as newer")
	}
}

func TestLatestVersion(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.0.6"}`))
	}))
	defer srv.Close()
	t.Setenv("PANEL_LATEST_URL", srv.URL)

	svc := NewChecker()
	if got := svc.Latest(t.Context()); got != "0.0.6" {
		t.Fatalf("Latest() = %q, want 0.0.6", got)
	}
	// A second call must come from the cache, so point the endpoint elsewhere
	// and confirm the answer does not change.
	t.Setenv("PANEL_LATEST_URL", "http://127.0.0.1:1")
	if got := svc.Latest(t.Context()); got != "0.0.6" {
		t.Fatalf("cached Latest() = %q, want 0.0.6", got)
	}
}

func TestLatestVersionUnavailable(t *testing.T) {
	t.Setenv("PANEL_LATEST_URL", "http://127.0.0.1:1")
	svc := NewChecker()
	if got := svc.Latest(t.Context()); got != "" {
		t.Fatalf("Latest() = %q, want empty", got)
	}
}

func TestLatestVersionDisabled(t *testing.T) {
	t.Setenv("PANEL_LATEST_URL", "")
	svc := NewChecker()
	if got := svc.Latest(t.Context()); got != "" {
		t.Fatalf("Latest() = %q, want empty", got)
	}
}

func TestLatestVersionRejectsBadPayload(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"not-a-version"}`))
	}))
	defer srv.Close()
	t.Setenv("PANEL_LATEST_URL", srv.URL)
	if got := NewChecker().Latest(t.Context()); got != "" {
		t.Fatalf("Latest() = %q, want empty", got)
	}
}
