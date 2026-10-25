package config

import (
	"fmt"
	"net"
	"os"
	"path/filepath"
	"strconv"
	"strings"
)

// Config holds process-level configuration. Everything that can be changed at
// runtime lives in the SQLite settings table instead.
type Config struct {
	Version        string
	Addr           string
	DataDir        string
	DefaultHugoDir string
	DefaultHugoBin string
	CookieSecure   string // auto, true, false
	LogLevel       string
	PublicURL      string
	SMTP           SMTPEnv
}

type SMTPEnv struct {
	Enabled  *bool
	Host     string
	Port     string
	Username string
	Password string
	From     string
	FromName string
	Security string
}

// Load reads environment variables and applies safe defaults. Relative paths
// are resolved against the current working directory, so running the binary in
// /app/hugo-panel naturally uses /app/hugo-panel/data.
func Load() (*Config, error) {
	port := env("PORT", "8080")
	if _, err := strconv.Atoi(port); err != nil {
		return nil, fmt.Errorf("invalid PORT %q", port)
	}

	dataDir := env("DATA_DIR", "./data")
	dataDir, err := filepath.Abs(dataDir)
	if err != nil {
		return nil, fmt.Errorf("resolve DATA_DIR: %w", err)
	}

	hugoDir := env("HUGO_DIR", "./hugo")
	if strings.HasPrefix(hugoDir, "~/") {
		if home, herr := os.UserHomeDir(); herr == nil {
			hugoDir = filepath.Join(home, hugoDir[2:])
		}
	}

	cfg := &Config{
		Addr:           env("ADDR", ":"+port),
		DataDir:        dataDir,
		DefaultHugoDir: hugoDir,
		DefaultHugoBin: env("HUGO_BINARY", "hugo"),
		CookieSecure:   strings.ToLower(env("COOKIE_SECURE", "auto")),
		LogLevel:       strings.ToLower(env("LOG_LEVEL", "info")),
		PublicURL:      strings.TrimRight(strings.TrimSpace(os.Getenv("PUBLIC_URL")), "/"),
		SMTP: SMTPEnv{
			Host:     os.Getenv("SMTP_HOST"),
			Port:     os.Getenv("SMTP_PORT"),
			Username: os.Getenv("SMTP_USERNAME"),
			Password: os.Getenv("SMTP_PASSWORD"),
			From:     os.Getenv("SMTP_FROM"),
			FromName: os.Getenv("SMTP_FROM_NAME"),
			Security: strings.ToLower(os.Getenv("SMTP_SECURITY")),
		},
	}

	if raw, ok := os.LookupEnv("SMTP_ENABLED"); ok {
		v := strings.EqualFold(raw, "true") || raw == "1" || strings.EqualFold(raw, "yes")
		cfg.SMTP.Enabled = &v
	}
	if cfg.CookieSecure != "auto" && cfg.CookieSecure != "true" && cfg.CookieSecure != "false" {
		return nil, fmt.Errorf("invalid COOKIE_SECURE %q (auto, true or false)", cfg.CookieSecure)
	}
	if cfg.SMTP.Port != "" {
		n, err := strconv.Atoi(cfg.SMTP.Port)
		if err != nil || n < 1 || n > 65535 {
			return nil, fmt.Errorf("invalid SMTP_PORT %q", cfg.SMTP.Port)
		}
	}
	switch cfg.SMTP.Security {
	case "", "none", "starttls", "tls":
	default:
		return nil, fmt.Errorf("invalid SMTP_SECURITY %q (none, starttls or tls)", cfg.SMTP.Security)
	}
	if !validAddr(cfg.Addr) {
		return nil, fmt.Errorf("invalid ADDR %q", cfg.Addr)
	}
	return cfg, nil
}

func (c *Config) ListenAddr() string {
	if strings.HasPrefix(c.Addr, ":") {
		return c.Addr
	}
	return c.Addr
}

func env(key, fallback string) string {
	if v, ok := os.LookupEnv(key); ok && strings.TrimSpace(v) != "" {
		return strings.TrimSpace(v)
	}
	return fallback
}

func validAddr(addr string) bool {
	_, port, err := net.SplitHostPort(addr)
	if err != nil {
		return false
	}
	n, err := strconv.Atoi(port)
	return err == nil && n > 0 && n < 65536
}
