package ai

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"
)

func TestConfigEndpointAndConfigured(t *testing.T) {
	cfg := Config{Enabled: true, BaseURL: " https://api.example.com/v1/ ", Model: " test-model "}.Normalized()
	if cfg.BaseURL != "https://api.example.com/v1" || cfg.Model != "test-model" {
		t.Fatalf("unexpected normalized config: %#v", cfg)
	}
	if !cfg.Configured() {
		t.Fatal("expected config to be configured")
	}
	endpoint, err := cfg.Endpoint()
	if err != nil {
		t.Fatal(err)
	}
	if endpoint != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("endpoint = %q", endpoint)
	}
	full := Config{Enabled: true, BaseURL: "https://api.example.com/v1/chat/completions", Model: "m"}
	if endpoint, _ := full.Endpoint(); endpoint != "https://api.example.com/v1/chat/completions" {
		t.Fatalf("full endpoint = %q", endpoint)
	}
	if (Config{BaseURL: "ftp://example.com", Model: "m", Enabled: true}).Configured() {
		t.Fatal("ftp endpoint must not be configured")
	}
	if (Config{BaseURL: "https://example.com", Model: "m"}).Configured() {
		t.Fatal("disabled config must not be configured")
	}
}

func TestCompleteSendsChatRequest(t *testing.T) {
	var gotPath, gotAuth string
	var got map[string]any
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotPath = r.URL.Path
		gotAuth = r.Header.Get("Authorization")
		if err := json.NewDecoder(r.Body).Decode(&got); err != nil {
			t.Errorf("decode request: %v", err)
		}
		_, _ = w.Write([]byte(`{"choices":[{"message":{"role":"assistant","content":"  Hello  "},"finish_reason":"stop"}]}`))
	}))
	defer srv.Close()

	client := New()
	client.HTTP = srv.Client()
	client.HTTP.Timeout = 5 * time.Second
	out, err := client.Complete(context.Background(), Config{
		Enabled: true, BaseURL: srv.URL + "/v1", Model: "test-model", APIKey: "secret-key",
	}, []Message{{Role: "user", Content: "hi"}}, 128, 0.2)
	if err != nil {
		t.Fatal(err)
	}
	if out != "Hello" {
		t.Fatalf("output = %q", out)
	}
	if gotPath != "/v1/chat/completions" {
		t.Fatalf("path = %q", gotPath)
	}
	if gotAuth != "Bearer secret-key" {
		t.Fatalf("authorization = %q", gotAuth)
	}
	if got["model"] != "test-model" {
		t.Fatalf("model = %#v", got["model"])
	}
	if got["stream"] != false {
		t.Fatalf("stream = %#v", got["stream"])
	}
	messages, _ := got["messages"].([]any)
	if len(messages) != 1 {
		t.Fatalf("messages = %#v", got["messages"])
	}
}

func TestCompleteReportsProviderErrors(t *testing.T) {
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusUnauthorized)
		_, _ = w.Write([]byte(`{"error":{"message":"invalid api key"}}`))
	}))
	defer srv.Close()

	client := New()
	client.HTTP = srv.Client()
	_, err := client.Complete(context.Background(), Config{
		Enabled: true, BaseURL: srv.URL, Model: "m", APIKey: "top-secret",
	}, []Message{{Role: "user", Content: "hi"}}, 32, 0)
	if err == nil {
		t.Fatal("expected an error")
	}
	if !strings.Contains(err.Error(), "invalid api key") {
		t.Fatalf("error = %v", err)
	}
	if strings.Contains(err.Error(), "top-secret") {
		t.Fatalf("error leaked the API key: %v", err)
	}
}

func TestCompleteRejectsEmptyAndTruncated(t *testing.T) {
	empty := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[]}`))
	}))
	defer empty.Close()
	client := New()
	client.HTTP = empty.Client()
	cfg := Config{Enabled: true, BaseURL: empty.URL, Model: "m"}
	if _, err := client.Complete(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}, 32, 0); err == nil {
		t.Fatal("expected an empty-response error")
	}

	truncated := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		_, _ = w.Write([]byte(`{"choices":[{"message":{"content":"partial"},"finish_reason":"length"}]}`))
	}))
	defer truncated.Close()
	client.HTTP = truncated.Client()
	cfg.BaseURL = truncated.URL
	if _, err := client.Complete(context.Background(), cfg, []Message{{Role: "user", Content: "hi"}}, 32, 0); err == nil {
		t.Fatal("expected a truncation error")
	}
}
