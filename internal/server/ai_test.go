package server

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
)

// newAIMock returns an OpenAI-compatible test server that always answers with
// the provided text.
func newAIMock(t *testing.T, reply string) *httptest.Server {
	t.Helper()
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path != "/v1/chat/completions" {
			t.Errorf("unexpected path %q", r.URL.Path)
		}
		if r.Method != http.MethodPost {
			t.Errorf("unexpected method %q", r.Method)
		}
		if auth := r.Header.Get("Authorization"); auth != "Bearer ai-secret-key" {
			t.Errorf("unexpected authorization %q", auth)
		}
		payload := map[string]any{}
		if err := json.NewDecoder(r.Body).Decode(&payload); err != nil {
			t.Errorf("decode request: %v", err)
		}
		if payload["model"] != "test-model" {
			t.Errorf("model = %#v", payload["model"])
		}
		encoded, _ := json.Marshal(map[string]any{
			"choices": []map[string]any{{
				"message":       map[string]any{"role": "assistant", "content": reply},
				"finish_reason": "stop",
			}},
		})
		_, _ = w.Write(encoded)
	}))
}

func saveAISettings(t *testing.T, app *testApp, csrf, baseURL string) map[string]any {
	t.Helper()
	res, view := app.request("PUT", "/api/settings", map[string]any{
		"ai_enabled":  true,
		"ai_base_url": baseURL,
		"ai_model":    "test-model",
		"ai_api_key":  "ai-secret-key",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save AI settings status %d: %#v", res.StatusCode, view)
	}
	return view
}

func TestAISettingsRedactKey(t *testing.T) {
	app, csrf := setupApp(t)
	mock := newAIMock(t, "OK")
	defer mock.Close()

	view := saveAISettings(t, app, csrf, mock.URL+"/v1")
	encoded, _ := json.Marshal(view)
	if strings.Contains(string(encoded), "ai-secret-key") || strings.Contains(string(encoded), "ai_api_key") {
		t.Fatal("settings response leaked the AI API key")
	}
	aiView, _ := view["ai"].(map[string]any)
	if aiView == nil || aiView["api_key_set"] != true || aiView["model"] != "test-model" || aiView["enabled"] != true {
		t.Fatalf("unexpected AI view: %#v", view["ai"])
	}
	if aiView["base_url"] != mock.URL+"/v1" {
		t.Fatalf("base_url = %#v", aiView["base_url"])
	}

	res, fetched := app.request("GET", "/api/settings", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("get settings status %d", res.StatusCode)
	}
	encoded, _ = json.Marshal(fetched)
	if strings.Contains(string(encoded), "ai-secret-key") {
		t.Fatal("GET /api/settings leaked the AI API key")
	}
}

func TestAISettingsValidation(t *testing.T) {
	app, csrf := setupApp(t)
	for _, body := range []map[string]any{
		{"ai_base_url": "ftp://example.com"},
		{"ai_model": "bad\nmodel"},
		{"ai_api_key": "bad\nkey"},
	} {
		res, _ := app.request("PUT", "/api/settings", body, map[string]string{"X-CSRF-Token": csrf})
		if res.StatusCode != http.StatusBadRequest {
			t.Fatalf("expected %#v to be rejected, got %d", body, res.StatusCode)
		}
	}
}

func TestAICompleteRequiresConfiguration(t *testing.T) {
	app, csrf := setupApp(t)
	res, body := app.request("POST", "/api/ai/complete", map[string]any{
		"task": "polish", "text": "draft",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest || body["error"] != "ai_not_configured" {
		t.Fatalf("unexpected response %d: %#v", res.StatusCode, body)
	}
}

func TestAICompleteTasks(t *testing.T) {
	app, csrf := setupApp(t)
	mock := newAIMock(t, "Polished Markdown")
	defer mock.Close()
	saveAISettings(t, app, csrf, mock.URL+"/v1")

	res, body := app.request("POST", "/api/ai/complete", map[string]any{
		"task": "polish", "text": "# Draft\n\nsome text", "title": "Title",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("polish status %d: %#v", res.StatusCode, body)
	}
	if body["text"] != "Polished Markdown" || body["model"] != "test-model" {
		t.Fatalf("unexpected polish body: %#v", body)
	}

	res, body = app.request("POST", "/api/ai/complete", map[string]any{
		"task": "custom", "text": "content",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("custom without instruction should fail, got %d: %#v", res.StatusCode, body)
	}

	res, body = app.request("POST", "/api/ai/complete", map[string]any{
		"task": "bogus", "text": "content",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("unknown task should fail, got %d: %#v", res.StatusCode, body)
	}
}

func TestAITestConnection(t *testing.T) {
	app, csrf := setupApp(t)
	mock := newAIMock(t, "OK")
	defer mock.Close()

	// Unsaved values can be tested before saving.
	res, body := app.request("POST", "/api/ai/test", map[string]any{
		"base_url": mock.URL + "/v1", "model": "test-model", "api_key": "ai-secret-key",
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK || body["ok"] != true {
		t.Fatalf("unexpected test response %d: %#v", res.StatusCode, body)
	}
	if body["model"] != "test-model" {
		t.Fatalf("model = %#v", body["model"])
	}
}

func TestContentSlugUsesAI(t *testing.T) {
	app, csrf := setupApp(t)
	mock := newAIMock(t, "How to write a blog")
	defer mock.Close()
	saveAISettings(t, app, csrf, mock.URL+"/v1")

	res, body := app.request("POST", "/api/content/slug", map[string]any{"title": "如何写博客"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("slug status %d: %#v", res.StatusCode, body)
	}
	if body["slug"] != "how-to-write-a-blog" || body["translated"] != true || body["source"] != "ai" || body["reason"] != "" {
		t.Fatalf("unexpected slug body: %#v", body)
	}
}

func TestContentSlugFallsBackFromTranslationToAI(t *testing.T) {
	app, csrf := setupApp(t)
	mock := newAIMock(t, "AI translated title")
	defer mock.Close()
	saveAISettings(t, app, csrf, mock.URL+"/v1")

	dead := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {}))
	deadURL := dead.URL
	dead.Close()
	res, view := app.request("PUT", "/api/settings", map[string]any{
		"translate_provider": "libretranslate",
		"translate_endpoint": deadURL,
	}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("save translation settings status %d: %#v", res.StatusCode, view)
	}

	res, body := app.request("POST", "/api/content/slug", map[string]any{"title": "标题"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("slug status %d: %#v", res.StatusCode, body)
	}
	if body["slug"] != "ai-translated-title" || body["source"] != "ai" {
		t.Fatalf("expected AI fallback, got %#v", body)
	}
}

func TestContentDirectoryEndpoint(t *testing.T) {
	app, csrf := setupApp(t)
	res, body := app.request("POST", "/api/content/directory", map[string]any{"path": "posts/2026"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusOK {
		t.Fatalf("create directory status %d: %#v", res.StatusCode, body)
	}
	if body["path"] != "posts/2026" {
		t.Fatalf("path = %#v", body["path"])
	}

	res, meta := app.request("GET", "/api/content/meta", nil, nil)
	if res.StatusCode != http.StatusOK {
		t.Fatalf("meta status %d", res.StatusCode)
	}
	dirs, _ := meta["directories"].([]any)
	found := false
	for _, dir := range dirs {
		if dir == "posts/2026" {
			found = true
		}
	}
	if !found {
		t.Fatalf("directories = %#v", meta["directories"])
	}

	res, body = app.request("POST", "/api/content/directory", map[string]any{"path": "../escape"}, map[string]string{"X-CSRF-Token": csrf})
	if res.StatusCode != http.StatusBadRequest {
		t.Fatalf("invalid path should fail, got %d: %#v", res.StatusCode, body)
	}
}
