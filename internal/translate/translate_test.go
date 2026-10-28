package translate

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"
)

func TestNormalizedFillsProviderEndpoints(t *testing.T) {
	libre := Config{Provider: " LibreTranslate "}.Normalized()
	if libre.Provider != ProviderLibreTranslate || libre.Endpoint != DefaultLibreEndpoint {
		t.Fatalf("unexpected libre config: %#v", libre)
	}
	if deepl := (Config{Provider: "deepl"}).Normalized(); deepl.Endpoint != DefaultDeepLEndpoint {
		t.Fatalf("unexpected deepl config: %#v", deepl)
	}
	if (Config{}).Configured() {
		t.Fatal("empty config must not be configured")
	}
	if (Config{Provider: "unknown", Endpoint: "https://example.com"}).Configured() {
		t.Fatal("unknown provider must not be configured")
	}
}

func TestLibreTranslateRequest(t *testing.T) {
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != http.MethodPost {
			t.Errorf("method = %s", r.Method)
		}
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"translatedText":"Hello World"}`))
	}))
	defer srv.Close()

	client := New()
	client.HTTP = srv.Client()
	client.HTTP.Timeout = 5 * time.Second
	out, err := client.Translate(context.Background(), Config{
		Provider: ProviderLibreTranslate, Endpoint: srv.URL, APIKey: "secret",
	}, "你好世界", "en")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello World" {
		t.Fatalf("translated = %q", out)
	}
	if got["q"] != "你好世界" || got["target"] != "en" || got["source"] != "auto" || got["api_key"] != "secret" {
		t.Fatalf("unexpected payload: %#v", got)
	}
}

func TestDeepLRequest(t *testing.T) {
	var auth string
	var payload map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		auth = r.Header.Get("Authorization")
		_ = json.NewDecoder(r.Body).Decode(&payload)
		_, _ = w.Write([]byte(`{"translations":[{"text":"Hello World"}]}`))
	}))
	defer srv.Close()

	client := New()
	client.HTTP = srv.Client()
	out, err := client.Translate(context.Background(), Config{
		Provider: ProviderDeepL, Endpoint: srv.URL, APIKey: "deepl-key",
	}, "你好世界", "en")
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello World" {
		t.Fatalf("translated = %q", out)
	}
	if auth != "DeepL-Auth-Key deepl-key" {
		t.Fatalf("authorization header = %q", auth)
	}
	if payload["target_lang"] != "EN-US" {
		t.Fatalf("unexpected payload: %#v", payload)
	}
}

func TestTranslateRejectsErrorsAndEmptyResults(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		http.Error(w, "rate limited", http.StatusTooManyRequests)
	}))
	defer srv.Close()

	client := New()
	client.HTTP = srv.Client()
	if _, err := client.Translate(context.Background(), Config{
		Provider: ProviderLibreTranslate, Endpoint: srv.URL,
	}, "hello", "en"); err == nil {
		t.Fatal("expected an error for a failed request")
	}

	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"translatedText":""}`))
	}))
	defer empty.Close()
	client.HTTP = empty.Client()
	if _, err := client.Translate(context.Background(), Config{
		Provider: ProviderLibreTranslate, Endpoint: empty.URL,
	}, "hello", "en"); err == nil {
		t.Fatal("expected an error for an empty translation")
	}

	if _, err := client.Translate(context.Background(), Config{
		Provider: "bogus", Endpoint: "https://example.com",
	}, "hello", "en"); err == nil {
		t.Fatal("expected an error for an unsupported provider")
	}
}
