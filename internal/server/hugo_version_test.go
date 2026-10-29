package server

import (
	"net/http"
	"os"
	"path/filepath"
	"testing"
)

func writeFakeHugo(t *testing.T, output string) string {
	t.Helper()
	path := filepath.Join(t.TempDir(), "hugo")
	script := "#!/bin/sh\necho \"" + output + "\"\n"
	if err := os.WriteFile(path, []byte(script), 0o755); err != nil {
		t.Fatal(err)
	}
	return path
}

func saveHugoBinary(t *testing.T, app *testApp, csrf, binary string) {
	t.Helper()
	res, body := app.request("PUT", "/api/settings", map[string]any{"hugo_binary": binary}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save hugo binary status %d: %#v", res.StatusCode, body)
	}
}

func TestHugoInitRejectsOutdatedHugo(t *testing.T) {
	app, csrf := setupApp(t)
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.150.1+extended linux/amd64"))

	res, body := app.request("POST", "/api/hugo/init", map[string]any{
		"title": "My site", "language": "en",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusPreconditionFailed {
		t.Fatalf("status = %d, want 412: %#v", res.StatusCode, body)
	}
	if body["error"] != "hugo_outdated" {
		t.Fatalf("error = %#v", body["error"])
	}

	res, meta := app.request("GET", "/api/hugo/config", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("config status %d", res.StatusCode)
	}
	if meta["exists"] != false {
		t.Fatalf("outdated Hugo must not create a site: %#v", meta["exists"])
	}
}

func TestHugoInitAcceptsCurrentHugo(t *testing.T) {
	app, csrf := setupApp(t)
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.169.0+extended linux/amd64 BuildDate=2026-10-01"))

	res, body := app.request("POST", "/api/hugo/init", map[string]any{
		"title": "My site", "base_url": "https://example.com/", "language": "en",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
	if body["exists"] != true || body["site_title"] != "My site" {
		t.Fatalf("unexpected config: %#v", body)
	}

	res, sys := app.request("GET", "/api/system", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("system status %d", res.StatusCode)
	}
	if sys["hugo_version"] != "0.169.0" || sys["hugo_supported"] != true {
		t.Fatalf("unexpected system hugo info: %#v", sys)
	}
	if sys["hugo_min_version"] == "" {
		t.Fatalf("missing hugo_min_version: %#v", sys)
	}
}

func TestHugoInitWithoutBinary(t *testing.T) {
	app, csrf := setupApp(t)
	saveHugoBinary(t, app, csrf, filepath.Join(t.TempDir(), "missing-hugo"))
	res, body := app.request("POST", "/api/hugo/init", map[string]any{
		"title": "My site", "language": "en",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusServiceUnavailable || body["error"] != "hugo_not_found" {
		t.Fatalf("unexpected response %d: %#v", res.StatusCode, body)
	}
}
