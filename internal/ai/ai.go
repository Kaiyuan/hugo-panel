// Package ai provides a compact client for OpenAI-compatible chat
// completion APIs. The API key is a server-side secret and must never be
// returned by the settings API.
package ai

import (
	"bytes"
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"strings"
	"time"
)

const (
	DefaultBaseURL = "https://api.openai.com/v1"
	DefaultModel   = "gpt-4o-mini"

	maxResponseBytes = 4 << 20
)

// Config holds the effective AI settings.
type Config struct {
	Enabled bool
	BaseURL string
	Model   string
	APIKey  string
}

// Normalized trims user input and removes trailing slashes from the base URL.
func (c Config) Normalized() Config {
	return Config{
		Enabled: c.Enabled,
		BaseURL: strings.TrimRight(strings.TrimSpace(c.BaseURL), "/"),
		Model:   strings.TrimSpace(c.Model),
		APIKey:  strings.TrimSpace(c.APIKey),
	}
}

// Endpoint resolves the chat completions URL. Both a base URL
// ("https://api.openai.com/v1") and a full endpoint are accepted.
func (c Config) Endpoint() (string, error) {
	c = c.Normalized()
	u, err := url.Parse(c.BaseURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return "", fmt.Errorf("invalid AI base URL")
	}
	if strings.HasSuffix(strings.TrimRight(u.Path, "/"), "/chat/completions") {
		return u.String(), nil
	}
	return strings.TrimRight(c.BaseURL, "/") + "/chat/completions", nil
}

// Configured reports whether AI requests can be made.
func (c Config) Configured() bool {
	c = c.Normalized()
	if !c.Enabled || c.Model == "" {
		return false
	}
	_, err := c.Endpoint()
	return err == nil
}

// Message is one chat message.
type Message struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

// Client performs chat completion requests. Use New to create one.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a generous timeout for long completions.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 90 * time.Second}}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 90 * time.Second}
}

// Complete sends the messages to the provider and returns the reply text.
func (c *Client) Complete(ctx context.Context, cfg Config, messages []Message, maxTokens int, temperature float64) (string, error) {
	cfg = cfg.Normalized()
	if cfg.Model == "" {
		return "", fmt.Errorf("AI model is not configured")
	}
	if len(messages) == 0 {
		return "", fmt.Errorf("no messages to send")
	}
	endpoint, err := cfg.Endpoint()
	if err != nil {
		return "", err
	}
	if maxTokens <= 0 {
		maxTokens = 1024
	}
	payload := map[string]any{
		"model":       cfg.Model,
		"messages":    messages,
		"max_tokens":  maxTokens,
		"temperature": temperature,
		"stream":      false,
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return "", err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, endpoint, bytes.NewReader(body))
	if err != nil {
		return "", err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HugoPanel")
	if cfg.APIKey != "" {
		req.Header.Set("Authorization", "Bearer "+cfg.APIKey)
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return "", fmt.Errorf("AI request failed: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return "", fmt.Errorf("read AI response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		msg := providerError(data)
		if msg == "" {
			msg = fmt.Sprintf("provider returned status %d", res.StatusCode)
		}
		return "", fmt.Errorf("AI provider error: %s", msg)
	}
	var out struct {
		Choices []struct {
			Message      Message `json:"message"`
			FinishReason string  `json:"finish_reason"`
		} `json:"choices"`
	}
	if err := json.Unmarshal(data, &out); err != nil {
		return "", fmt.Errorf("decode AI response: %w", err)
	}
	if len(out.Choices) == 0 {
		return "", fmt.Errorf("AI provider returned an empty response")
	}
	content := strings.TrimSpace(out.Choices[0].Message.Content)
	if content == "" {
		return "", fmt.Errorf("AI provider returned an empty response")
	}
	if out.Choices[0].FinishReason == "length" {
		return "", fmt.Errorf("AI response was truncated; shorten the text or use a model with a larger output limit")
	}
	return content, nil
}

func providerError(data []byte) string {
	var out struct {
		Error struct {
			Message string `json:"message"`
		} `json:"error"`
	}
	if json.Unmarshal(data, &out) == nil && strings.TrimSpace(out.Error.Message) != "" {
		return truncate(strings.TrimSpace(out.Error.Message), 300)
	}
	return truncate(strings.TrimSpace(string(data)), 300)
}

func truncate(s string, limit int) string {
	if len([]rune(s)) <= limit {
		return s
	}
	return string([]rune(s)[:limit]) + "…"
}
