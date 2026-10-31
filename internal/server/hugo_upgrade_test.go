package server

import (
	"archive/tar"
	"bytes"
	"compress/gzip"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"runtime"
	"strings"
	"testing"
)

func fakeHugoArchive(t *testing.T, version string) []byte {
	t.Helper()
	script := "#!/bin/sh\n" +
		"if [ \"$1\" = \"version\" ]; then\n" +
		"  echo \"hugo v" + version + "+extended linux/amd64 BuildDate=unknown\"\n" +
		"  exit 0\n" +
		"fi\n" +
		"exit 0\n"
	var buf bytes.Buffer
	gz := gzip.NewWriter(&buf)
	tw := tar.NewWriter(gz)
	if err := tw.WriteHeader(&tar.Header{Name: "hugo", Mode: 0o755, Size: int64(len(script))}); err != nil {
		t.Fatal(err)
	}
	if _, err := tw.Write([]byte(script)); err != nil {
		t.Fatal(err)
	}
	if err := tw.Close(); err != nil {
		t.Fatal(err)
	}
	if err := gz.Close(); err != nil {
		t.Fatal(err)
	}
	return buf.Bytes()
}

func TestHugoUpgradeChecksThemesBeforeSwitching(t *testing.T) {
	if runtime.GOOS != "linux" {
		t.Skip("upgrade test uses the Linux Hugo release asset")
	}
	if runtime.GOARCH != "amd64" && runtime.GOARCH != "arm64" {
		t.Skip("unsupported test architecture")
	}
	app := newTestApp(t)
	res, state := app.request("POST", "/api/setup", map[string]any{
		"username": "admin", "password": "password123", "email": "", "language": "en",
	}, nil)
	if res.StatusCode != 200 {
		t.Fatalf("setup status %d", res.StatusCode)
	}
	csrf, _ := state["csrf"].(string)
	if csrf == "" {
		t.Fatal("missing CSRF token")
	}

	// Point the release lookup at local servers before the first /api/system
	// call, because that response checks the latest version and caches it.
	archive := fakeHugoArchive(t, "0.200.0")
	download := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		asset := "hugo_extended_0.200.0_linux-" + runtime.GOARCH + ".tar.gz"
		if !strings.HasSuffix(r.URL.Path, asset) {
			http.NotFound(w, r)
			return
		}
		w.Header().Set("Content-Type", "application/gzip")
		_, _ = w.Write(archive)
	}))
	defer download.Close()
	latest := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"tag_name":"v0.200.0"}`))
	}))
	defer latest.Close()
	t.Setenv("HUGO_LATEST_URL", latest.URL)
	t.Setenv("HUGO_DOWNLOAD_BASE", download.URL+"/releases/download")

	res, sys := app.request("GET", "/api/system", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("system status %d", res.StatusCode)
	}
	hugoDir, _ := sys["hugo_dir"].(string)
	dataDir, _ := sys["data_dir"].(string)
	if hugoDir == "" || dataDir == "" {
		t.Fatalf("missing paths in system response: %#v", sys)
	}
	themeDir := filepath.Join(hugoDir, "themes", "demo")
	if err := os.MkdirAll(themeDir, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(hugoDir, "hugo.toml"), []byte("baseURL = \"https://example.com/\"\ntheme = \"demo\"\n"), 0o644); err != nil {
		t.Fatal(err)
	}
	writeMinVersion := func(version string) {
		meta := "name = \"Demo\"\nmin_version = \"" + version + "\"\n"
		if err := os.WriteFile(filepath.Join(themeDir, "theme.toml"), []byte(meta), 0o644); err != nil {
			t.Fatal(err)
		}
	}
	writeMinVersion("0.250.0")

	res, body := app.request("POST", "/api/hugo/upgrade", map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusConflict || body["error"] != "incompatible" {
		t.Fatalf("expected incompatible upgrade, got %d %#v", res.StatusCode, body)
	}
	if _, ok := body["compatibility"].(map[string]any); !ok {
		t.Fatalf("compatibility details missing: %#v", body)
	}
	res, view := app.request("GET", "/api/settings", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("settings status %d", res.StatusCode)
	}
	if view["hugo_binary"] == filepath.Join(dataDir, "hugo-bin", "0.200.0", "hugo") {
		t.Fatal("panel switched to an incompatible Hugo binary")
	}

	writeMinVersion("0.150.0")
	res, body = app.request("POST", "/api/hugo/upgrade", map[string]any{}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 || body["upgraded"] != true {
		t.Fatalf("upgrade failed: %d %#v", res.StatusCode, body)
	}
	expected := filepath.Join(dataDir, "hugo-bin", "0.200.0", "hugo")
	if body["binary"] != expected {
		t.Fatalf("unexpected binary path: %#v", body["binary"])
	}
	res, view = app.request("GET", "/api/settings", nil, nil)
	if res.StatusCode != 200 || view["hugo_binary"] != expected {
		t.Fatalf("settings were not switched: %d %#v", res.StatusCode, view["hugo_binary"])
	}
}
