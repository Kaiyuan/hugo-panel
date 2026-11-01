package server

import (
	"net/http"
	"net/http/httptest"
	"testing"
)

func TestPanelVersionEndpoint(t *testing.T) {
	app, _ := setupApp(t)

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/" {
			http.Redirect(w, r, "/releases/tag/V0.0.6", http.StatusFound)
			return
		}
		w.Header().Set("Content-Type", "text/html; charset=utf-8")
		_, _ = w.Write([]byte("<html><body>latest release</body></html>"))
	}))
	defer upstream.Close()
	t.Setenv("PANEL_LATEST_URL", upstream.URL)

	res, body := app.request("GET", "/api/panel/version", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
	if body["version"] != "0.0.5" || body["latest_version"] != "0.0.6" {
		t.Fatalf("unexpected version payload: %#v", body)
	}
	if body["update_available"] != true {
		t.Fatalf("update_available = %#v", body["update_available"])
	}
	if body["release_url"] == "" {
		t.Fatal("release_url is missing")
	}
}

func TestPanelVersionEndpointWhenCheckDisabled(t *testing.T) {
	app, _ := setupApp(t)
	res, body := app.request("GET", "/api/panel/version", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
	if body["latest_version"] != "" || body["update_available"] != false {
		t.Fatalf("unexpected version payload: %#v", body)
	}
}
