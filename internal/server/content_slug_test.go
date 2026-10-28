package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

func setupApp(t *testing.T) (*testApp, string) {
	t.Helper()
	app := newTestApp(t)
	res, state := app.request("POST", "/api/setup", map[string]any{
		"username": "admin", "password": "password123", "email": "", "language": "en",
	}, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("setup status %d", res.StatusCode)
	}
	csrf, _ := state["csrf"].(string)
	if csrf == "" {
		t.Fatal("missing csrf token")
	}
	return app, csrf
}

func TestContentSlugWithoutProvider(t *testing.T) {
	app, csrf := setupApp(t)
	res, body := app.request("POST", "/api/content/slug", map[string]any{"title": "Hello, World!"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("status %d: %#v", res.StatusCode, body)
	}
	if body["slug"] != "hello-world" {
		t.Fatalf("slug = %#v", body["slug"])
	}
	if body["translated"] != false || body["reason"] != "not_configured" {
		t.Fatalf("unexpected body: %#v", body)
	}
}

func TestContentSlugUsesConfiguredProvider(t *testing.T) {
	app, csrf := setupApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"translatedText":"How to write a blog"}`))
	}))
	defer provider.Close()

	res, view := app.request("PUT", "/api/settings", map[string]any{
		"translate_provider": "libretranslate",
		"translate_endpoint": provider.URL,
		"translate_api_key":  "provider-secret-key",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save settings status %d: %#v", res.StatusCode, view)
	}
	if view["translate_api_key"] != nil {
		t.Fatal("settings view must not return the API key field")
	}
	translation, _ := view["translation"].(map[string]any)
	if translation == nil || translation["api_key_set"] != true {
		t.Fatalf("unexpected translation view: %#v", view["translation"])
	}
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "provider-secret-key") {
		t.Fatal("settings response leaked the translation API key")
	}

	res, body := app.request("POST", "/api/content/slug", map[string]any{"title": "如何写博客"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("slug status %d: %#v", res.StatusCode, body)
	}
	if body["slug"] != "how-to-write-a-blog" || body["translated"] != true {
		t.Fatalf("unexpected slug response: %#v", body)
	}
}

func TestContentSlugFallsBackWhenProviderFails(t *testing.T) {
	app, csrf := setupApp(t)
	provider := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "boom", http.StatusBadGateway)
	}))
	providerURL := provider.URL
	provider.Close()

	res, _ := app.request("PUT", "/api/settings", map[string]any{
		"translate_provider": "libretranslate",
		"translate_endpoint": providerURL,
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save settings status %d", res.StatusCode)
	}

	res, body := app.request("POST", "/api/content/slug", map[string]any{"title": "Hello World"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("slug status %d: %#v", res.StatusCode, body)
	}
	if body["slug"] != "hello-world" || body["translated"] != false || body["reason"] != "provider_failed" {
		t.Fatalf("unexpected fallback body: %#v", body)
	}
}

func TestTranslationSettingsValidation(t *testing.T) {
	app, csrf := setupApp(t)
	res, _ := app.request("PUT", "/api/settings", map[string]any{
		"translate_provider": "bogus",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid provider should be rejected, got %d", res.StatusCode)
	}
	res, _ = app.request("PUT", "/api/settings", map[string]any{
		"translate_endpoint": "ftp://example.com",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid endpoint should be rejected, got %d", res.StatusCode)
	}
}
