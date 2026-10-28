// Package translate provides a small HTTP client for the translation services
// used to turn article titles into English file names.
package translate

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
	ProviderLibreTranslate = "libretranslate"
	ProviderDeepL          = "deepl"

	DefaultLibreEndpoint = "https://libretranslate.com/translate"
	DefaultDeepLEndpoint = "https://api-free.deepl.com/v2/translate"

	maxResponseBytes = 1 << 20
)

// Config holds the effective translation settings. The API key is a secret and
// must never be exposed through the settings API.
type Config struct {
	Provider string
	Endpoint string
	APIKey   string
}

// Normalized trims user input and fills in the provider's default endpoint.
func (c Config) Normalized() Config {
	out := Config{
		Provider: strings.ToLower(strings.TrimSpace(c.Provider)),
		Endpoint: strings.TrimSpace(c.Endpoint),
		APIKey:   strings.TrimSpace(c.APIKey),
	}
	if out.Provider == ProviderLibreTranslate && out.Endpoint == "" {
		out.Endpoint = DefaultLibreEndpoint
	}
	if out.Provider == ProviderDeepL && out.Endpoint == "" {
		out.Endpoint = DefaultDeepLEndpoint
	}
	return out
}

// Configured reports whether a provider and endpoint are available.
func (c Config) Configured() bool {
	c = c.Normalized()
	switch c.Provider {
	case ProviderLibreTranslate, ProviderDeepL:
		return c.Endpoint != ""
	default:
		return false
	}
}

// Client performs translation requests. The zero value is not usable; use New.
type Client struct {
	HTTP *http.Client
}

// New returns a client with a conservative timeout.
func New() *Client {
	return &Client{HTTP: &http.Client{Timeout: 20 * time.Second}}
}

// Translate sends text to the configured provider and returns the translation.
func (c *Client) Translate(ctx context.Context, cfg Config, text, target string) (string, error) {
	cfg = cfg.Normalized()
	if strings.TrimSpace(text) == "" {
		return "", nil
	}
	if target == "" {
		target = "en"
	}
	switch cfg.Provider {
	case ProviderLibreTranslate:
		return c.libreTranslate(ctx, cfg, text, target)
	case ProviderDeepL:
		return c.deepL(ctx, cfg, text, target)
	default:
		return "", fmt.Errorf("unsupported translation provider %q", cfg.Provider)
	}
}

func (c *Client) httpClient() *http.Client {
	if c != nil && c.HTTP != nil {
		return c.HTTP
	}
	return &http.Client{Timeout: 20 * time.Second}
}

func (c *Client) libreTranslate(ctx context.Context, cfg Config, text, target string) (string, error) {
	payload := map[string]any{
		"q":      text,
		"source": "auto",
		"target": target,
		"format": "text",
	}
	if cfg.APIKey != "" {
		payload["api_key"] = cfg.APIKey
	}
	var out struct {
		TranslatedText string `json:"translatedText"`
	}
	if err := c.postJSON(ctx, cfg.Endpoint, payload, &out, nil); err != nil {
		return "", err
	}
	if strings.TrimSpace(out.TranslatedText) == "" {
		return "", fmt.Errorf("translation provider returned an empty result")
	}
	return out.TranslatedText, nil
}

func (c *Client) deepL(ctx context.Context, cfg Config, text, target string) (string, error) {
	lang := strings.ToUpper(target)
	if lang == "EN" {
		lang = "EN-US"
	}
	payload := map[string]any{
		"text":        []string{text},
		"target_lang": lang,
	}
	headers := map[string]string{"Authorization": "DeepL-Auth-Key " + cfg.APIKey}
	var out struct {
		Translations []struct {
			Text string `json:"text"`
		} `json:"translations"`
	}
	if err := c.postJSON(ctx, cfg.Endpoint, payload, &out, headers); err != nil {
		return "", err
	}
	if len(out.Translations) == 0 || strings.TrimSpace(out.Translations[0].Text) == "" {
		return "", fmt.Errorf("translation provider returned an empty result")
	}
	return out.Translations[0].Text, nil
}

func (c *Client) postJSON(ctx context.Context, endpoint string, payload any, out any, headers map[string]string) error {
	u, err := url.Parse(endpoint)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return fmt.Errorf("invalid translation endpoint")
	}
	body, err := json.Marshal(payload)
	if err != nil {
		return err
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, u.String(), bytes.NewReader(body))
	if err != nil {
		return err
	}
	req.Header.Set("Content-Type", "application/json")
	req.Header.Set("Accept", "application/json")
	req.Header.Set("User-Agent", "HugoPanel")
	for k, v := range headers {
		req.Header.Set(k, v)
	}
	res, err := c.httpClient().Do(req)
	if err != nil {
		return fmt.Errorf("translation request failed: %w", err)
	}
	defer res.Body.Close()
	data, err := io.ReadAll(io.LimitReader(res.Body, maxResponseBytes))
	if err != nil {
		return fmt.Errorf("read translation response: %w", err)
	}
	if res.StatusCode < 200 || res.StatusCode > 299 {
		msg := strings.TrimSpace(string(data))
		if len(msg) > 200 {
			msg = msg[:200]
		}
		if msg == "" {
			return fmt.Errorf("translation provider returned status %d", res.StatusCode)
		}
		return fmt.Errorf("translation provider returned status %d: %s", res.StatusCode, msg)
	}
	if err := json.Unmarshal(data, out); err != nil {
		return fmt.Errorf("decode translation response: %w", err)
	}
	return nil
}
