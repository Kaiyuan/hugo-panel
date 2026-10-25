package server

import (
	"bytes"
	"encoding/json"
	"io"
	"io/fs"
	"log/slog"
	"net/http"
	"net/http/cookiejar"
	"net/http/httptest"
	"path/filepath"
	"testing"

	"github.com/Kaiyuan/hugo-panel/internal/config"
	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/i18n"
	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
	"github.com/Kaiyuan/hugo-panel/web"
)

type testApp struct {
	t      *testing.T
	server *httptest.Server
	client *http.Client
}

func newTestApp(t *testing.T) *testApp {
	t.Helper()
	root := t.TempDir()
	dataDir := filepath.Join(root, "data")
	hugoDir := filepath.Join(root, "hugo")
	cfg := &config.Config{
		Addr:           ":0",
		DataDir:        dataDir,
		DefaultHugoDir: hugoDir,
		DefaultHugoBin: "hugo-not-installed",
		CookieSecure:   "false",
		LogLevel:       "error",
	}
	st, err := store.Open(t.Context(), filepath.Join(dataDir, "panel.db"))
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { st.Close() })
	set := settings.New(st, cfg)
	if err := set.EnsureDefaults(t.Context()); err != nil {
		t.Fatal(err)
	}
	locales, err := fs.Sub(web.Files, "locales")
	if err != nil {
		t.Fatal(err)
	}
	logger := slog.New(slog.NewTextHandler(io.Discard, nil))
	srv, err := New(cfg, st, set, i18n.New(locales, dataDir), mailer.New(), hugo.NewService(), logger, "test")
	if err != nil {
		t.Fatal(err)
	}
	ts := httptest.NewServer(srv.Handler())
	t.Cleanup(ts.Close)
	jar, _ := cookiejar.New(nil)
	return &testApp{t: t, server: ts, client: &http.Client{Jar: jar}}
}

func (a *testApp) request(method, path string, body any, headers map[string]string) (*http.Response, map[string]any) {
	a.t.Helper()
	var reader io.Reader
	if body != nil {
		b, err := json.Marshal(body)
		if err != nil {
			a.t.Fatal(err)
		}
		reader = bytes.NewReader(b)
	}
	req, err := http.NewRequest(method, a.server.URL+path, reader)
	if err != nil {
		a.t.Fatal(err)
	}
	if body != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := a.client.Do(req)
	if err != nil {
		a.t.Fatal(err)
	}
	var decoded map[string]any
	_ = json.NewDecoder(res.Body).Decode(&decoded)
	res.Body.Close()
	return res, decoded
}

func TestSetupLoginCSRFAndContentFlow(t *testing.T) {
	app := newTestApp(t)

	res, state := app.request("GET", "/api/state", nil, nil)
	if res.StatusCode != 200 || state["setup_required"] != true {
		t.Fatalf("initial state: %d %#v", res.StatusCode, state)
	}
	languages, _ := state["languages"].([]any)
	if len(languages) != 8 {
		t.Fatalf("expected 8 languages, got %d", len(languages))
	}
	firstLang, _ := languages[0].(map[string]any)
	if firstLang["code"] != "en" || firstLang["nativeName"] != "English" {
		t.Fatalf("unexpected language JSON contract: %#v", firstLang)
	}

	res, state = app.request("POST", "/api/setup", map[string]any{
		"username": "admin", "password": "password123", "email": "admin@example.com", "language": "en",
	}, nil)
	if res.StatusCode != 200 || state["authenticated"] != true {
		t.Fatalf("setup: %d %#v", res.StatusCode, state)
	}
	user, _ := state["user"].(map[string]any)
	if user["username"] != "admin" {
		t.Fatalf("setup user: %#v", user)
	}
	csrf, _ := state["csrf"].(string)
	if csrf == "" {
		t.Fatal("setup did not return a CSRF token")
	}

	res, _ = app.request("GET", "/api/me", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("me status %d", res.StatusCode)
	}

	content := map[string]any{
		"path": "posts/hello.md", "format": "yaml",
		"front_matter": map[string]any{"title": "Hello", "draft": false, "date": "2026-01-01"},
		"body":         "# Hello\n",
	}
	res, _ = app.request("POST", "/api/content", content, nil)
	if res.StatusCode != 403 {
		t.Fatalf("content without CSRF should be 403, got %d", res.StatusCode)
	}
	res, _ = app.request("POST", "/api/content", content, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 201 {
		t.Fatalf("content create status %d", res.StatusCode)
	}
	res, list := app.request("GET", "/api/content", nil, nil)
	if res.StatusCode != 200 || list["total"].(float64) != 1 {
		t.Fatalf("content list: %d %#v", res.StatusCode, list)
	}

	res, _ = app.request("POST", "/api/content", content, map[string]string{
		"X-CSRF-Token": csrf, "Origin": "https://evil.example",
	})
	if res.StatusCode != 403 {
		t.Fatalf("cross-origin request should be 403, got %d", res.StatusCode)
	}

	res, _ = app.request("POST", "/api/logout", nil, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != 200 {
		t.Fatalf("logout status %d", res.StatusCode)
	}
	res, state = app.request("GET", "/api/state", nil, nil)
	if state["authenticated"] != false {
		t.Fatalf("state after logout: %#v", state)
	}

	res, _ = app.request("POST", "/api/login", map[string]any{"username": "admin", "password": "wrong"}, nil)
	if res.StatusCode != 401 {
		t.Fatalf("wrong login status %d", res.StatusCode)
	}
	res, state = app.request("POST", "/api/login", map[string]any{"username": "admin", "password": "password123"}, nil)
	if res.StatusCode != 200 || state["authenticated"] != true {
		t.Fatalf("login: %d %#v", res.StatusCode, state)
	}
}

func TestI18NAndSettingsEndpoints(t *testing.T) {
	app := newTestApp(t)
	res, _ := app.request("POST", "/api/setup", map[string]any{
		"username": "admin", "password": "password123", "email": "", "language": "zh-CN",
	}, nil)
	if res.StatusCode != 200 {
		t.Fatalf("setup status %d", res.StatusCode)
	}
	res, body := app.request("GET", "/api/i18n/zh-CN", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("i18n status %d", res.StatusCode)
	}
	messages, _ := body["messages"].(map[string]any)
	if messages["nav.dashboard"] != "仪表盘" {
		t.Fatalf("unexpected translation: %#v", messages["nav.dashboard"])
	}
	res, view := app.request("GET", "/api/settings", nil, nil)
	if res.StatusCode != 200 {
		t.Fatalf("settings status %d", res.StatusCode)
	}
	if view["default_language"] != "zh-CN" {
		t.Fatalf("default language: %#v", view["default_language"])
	}
	if _, ok := view["smtp"].(map[string]any); !ok {
		t.Fatalf("smtp view missing: %#v", view)
	}
}
