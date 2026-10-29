package server

import (
	"net/http"
	"net/http/httptest"
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

func mockLatestHugo(t *testing.T, version string) {
	t.Helper()
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"tag_name":"v` + version + `"}`))
	}))
	t.Cleanup(srv.Close)
	t.Setenv("HUGO_LATEST_URL", srv.URL)
}

func TestSystemFlagsOutdatedHugo(t *testing.T) {
	app, csrf := setupApp(t)
	mockLatestHugo(t, "0.169.0")
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.150.1+extended linux/amd64"))

	res, sys := app.request("GET", "/api/system", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("system status %d", res.StatusCode)
	}
	if sys["hugo_version"] != "0.150.1" || sys["hugo_latest_version"] != "0.169.0" {
		t.Fatalf("unexpected versions: %#v", sys)
	}
	if sys["hugo_outdated"] != true {
		t.Fatalf("hugo_outdated = %#v", sys["hugo_outdated"])
	}
	if sys["hugo_update_url"] == "" {
		t.Fatalf("missing hugo_update_url: %#v", sys)
	}
}

func TestSystemAcceptsCurrentHugo(t *testing.T) {
	app, csrf := setupApp(t)
	mockLatestHugo(t, "0.169.0")
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.169.0+extended linux/amd64"))

	res, sys := app.request("GET", "/api/system", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("system status %d", res.StatusCode)
	}
	if sys["hugo_version"] != "0.169.0" || sys["hugo_outdated"] != false {
		t.Fatalf("unexpected system info: %#v", sys)
	}
}

func TestSystemWithoutLatestCheckNeverFlagsUpdate(t *testing.T) {
	app, csrf := setupApp(t)
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.150.1+extended linux/amd64"))

	res, sys := app.request("GET", "/api/system", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("system status %d", res.StatusCode)
	}
	if sys["hugo_latest_version"] != "" || sys["hugo_outdated"] != false {
		t.Fatalf("offline check should stay unknown: %#v", sys)
	}
}

func TestHugoInitIsAdvisory(t *testing.T) {
	app, csrf := setupApp(t)
	// Init only needs the site directory; an old or missing Hugo is a warning,
	// not a hard failure, because the version check is advisory.
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.150.1+extended linux/amd64"))

	res, body := app.request("POST", "/api/hugo/init", map[string]any{
		"title": "My site", "base_url": "https://example.com/", "language": "en",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
	if body["exists"] != true || body["site_title"] != "My site" {
		t.Fatalf("unexpected config: %#v", body)
	}
}

func TestHugoInitWithoutBinary(t *testing.T) {
	app, csrf := setupApp(t)
	saveHugoBinary(t, app, csrf, filepath.Join(t.TempDir(), "missing-hugo"))
	res, body := app.request("POST", "/api/hugo/init", map[string]any{
		"title": "My site", "language": "en",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
}

func TestDashboardReportsHugoUpdate(t *testing.T) {
	app, csrf := setupApp(t)
	mockLatestHugo(t, "0.169.0")
	saveHugoBinary(t, app, csrf, writeFakeHugo(t, "hugo v0.150.1+extended linux/amd64"))
	res, dash := app.request("GET", "/api/dashboard", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("dashboard status %d", res.StatusCode)
	}
	hugoInfo, _ := dash["hugo"].(map[string]any)
	if hugoInfo == nil {
		t.Fatalf("missing hugo info: %#v", dash)
	}
	if hugoInfo["latest_version"] != "0.169.0" || hugoInfo["outdated"] != true {
		t.Fatalf("unexpected dashboard hugo info: %#v", hugoInfo)
	}
}
