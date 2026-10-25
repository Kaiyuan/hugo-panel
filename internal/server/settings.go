package server

import (
	"context"
	"fmt"
	"html"
	"net/http"
	"net/mail"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

type settingsView struct {
	HugoDir         string   `json:"hugo_dir"`
	HugoBinary      string   `json:"hugo_binary"`
	PublicURL       string   `json:"public_url"`
	DefaultLanguage string   `json:"default_language"`
	DefaultTheme    string   `json:"default_theme"`
	Notifications   bool     `json:"notifications_enabled"`
	SMTP            smtpView `json:"smtp"`
	EnvOverrides    []string `json:"env_overrides"`
	SetupCompleted  string   `json:"setup_completed_at,omitempty"`
}

type smtpView struct {
	Enabled     bool   `json:"enabled"`
	Host        string `json:"host"`
	Port        int    `json:"port"`
	Username    string `json:"username"`
	From        string `json:"from"`
	FromName    string `json:"from_name"`
	Security    string `json:"security"`
	PasswordSet bool   `json:"password_set"`
}

func (s *Server) settingsView(ctx context.Context) (settingsView, error) {
	all, err := s.settings.All(ctx)
	if err != nil {
		return settingsView{}, err
	}
	smtpCfg, err := s.settings.EffectiveSMTP(ctx)
	if err != nil {
		return settingsView{}, err
	}
	overrides := append([]string{}, smtpCfg.Overrides...)
	if s.settings.PublicURLFromEnv() {
		overrides = append(overrides, settings.KeyPublicURL)
	}
	return settingsView{
		HugoDir:         all[settings.KeyHugoDir],
		HugoBinary:      all[settings.KeyHugoBinary],
		PublicURL:       s.settings.PublicURL(ctx),
		DefaultLanguage: all[settings.KeyDefaultLanguage],
		DefaultTheme:    all[settings.KeyDefaultTheme],
		Notifications:   parseBoolSetting(all[settings.KeyNotifications]),
		EnvOverrides:    overrides,
		SetupCompleted:  all[settings.KeySetupCompletedAt],
		SMTP: smtpView{
			Enabled:     smtpCfg.Enabled,
			Host:        smtpCfg.Host,
			Port:        smtpCfg.Port,
			Username:    smtpCfg.Username,
			From:        smtpCfg.From,
			FromName:    smtpCfg.FromName,
			Security:    smtpCfg.Security,
			PasswordSet: smtpCfg.Password != "",
		},
	}, nil
}

func (s *Server) handleSettingsGet(w http.ResponseWriter, r *http.Request, sess store.Session) {
	view, err := s.settingsView(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load settings")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type settingsPatch struct {
	HugoDir           *string `json:"hugo_dir"`
	HugoBinary        *string `json:"hugo_binary"`
	PublicURL         *string `json:"public_url"`
	DefaultLanguage   *string `json:"default_language"`
	DefaultTheme      *string `json:"default_theme"`
	Notifications     *bool   `json:"notifications_enabled"`
	SMTPEnabled       *bool   `json:"smtp_enabled"`
	SMTPHost          *string `json:"smtp_host"`
	SMTPPort          *int    `json:"smtp_port"`
	SMTPUsername      *string `json:"smtp_username"`
	SMTPPassword      *string `json:"smtp_password"`
	ClearSMTPPassword bool    `json:"clear_smtp_password"`
	SMTPFrom          *string `json:"smtp_from"`
	SMTPFromName      *string `json:"smtp_from_name"`
	SMTPSecurity      *string `json:"smtp_security"`
}

func (s *Server) handleSettingsSave(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req settingsPatch
	if !decodeJSON(w, r, &req, 256*1024) {
		return
	}
	values := map[string]string{}
	if req.HugoDir != nil {
		dir := strings.TrimSpace(*req.HugoDir)
		if dir == "" || strings.ContainsAny(dir, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Hugo directory is required")
			return
		}
		abs, err := filepath.Abs(dir)
		if err != nil {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid Hugo directory")
			return
		}
		if err := os.MkdirAll(abs, 0o755); err != nil {
			writeError(w, http.StatusBadRequest, "invalid_path", "Could not create or access the Hugo directory")
			return
		}
		values[settings.KeyHugoDir] = abs
	}
	if req.HugoBinary != nil {
		bin := strings.TrimSpace(*req.HugoBinary)
		if bin == "" || strings.ContainsAny(bin, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Hugo binary is required")
			return
		}
		values[settings.KeyHugoBinary] = bin
	}
	if req.PublicURL != nil {
		pub := strings.TrimRight(strings.TrimSpace(*req.PublicURL), "/")
		if pub != "" {
			u, err := url.Parse(pub)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "Public URL must be an absolute http(s) URL")
				return
			}
		}
		values[settings.KeyPublicURL] = pub
	}
	if req.DefaultLanguage != nil {
		lang := strings.TrimSpace(*req.DefaultLanguage)
		if !s.languageExists(lang) {
			writeError(w, http.StatusBadRequest, "invalid_locale", "Unsupported language")
			return
		}
		values[settings.KeyDefaultLanguage] = lang
	}
	if req.DefaultTheme != nil {
		theme := strings.TrimSpace(*req.DefaultTheme)
		if theme != "system" && theme != "light" && theme != "dark" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid default theme")
			return
		}
		values[settings.KeyDefaultTheme] = theme
	}
	if req.Notifications != nil {
		values[settings.KeyNotifications] = strconv.FormatBool(*req.Notifications)
	}
	if req.SMTPEnabled != nil {
		values[settings.KeySMTPEnabled] = strconv.FormatBool(*req.SMTPEnabled)
	}
	if req.SMTPHost != nil {
		host := strings.TrimSpace(*req.SMTPHost)
		if strings.ContainsAny(host, " \r\n\t/\\") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid SMTP host")
			return
		}
		values[settings.KeySMTPHost] = host
	}
	if req.SMTPPort != nil {
		if *req.SMTPPort < 1 || *req.SMTPPort > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_request", "SMTP port must be between 1 and 65535")
			return
		}
		values[settings.KeySMTPPort] = strconv.Itoa(*req.SMTPPort)
	}
	if req.SMTPUsername != nil {
		v := strings.TrimSpace(*req.SMTPUsername)
		if strings.ContainsAny(v, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid SMTP username")
			return
		}
		values[settings.KeySMTPUsername] = v
	}
	if req.ClearSMTPPassword {
		values[settings.KeySMTPPassword] = ""
	} else if req.SMTPPassword != nil && *req.SMTPPassword != "" {
		if strings.ContainsAny(*req.SMTPPassword, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid SMTP password")
			return
		}
		values[settings.KeySMTPPassword] = *req.SMTPPassword
	}
	if req.SMTPFrom != nil {
		from := strings.TrimSpace(*req.SMTPFrom)
		if from != "" {
			addr, err := mail.ParseAddress(from)
			if err != nil || addr.Name != "" {
				writeError(w, http.StatusBadRequest, "invalid_email", "Invalid sender email address")
				return
			}
			from = addr.Address
		}
		values[settings.KeySMTPFrom] = from
	}
	if req.SMTPFromName != nil {
		name := strings.TrimSpace(*req.SMTPFromName)
		if strings.ContainsAny(name, "\x00\r\n") || len([]rune(name)) > 120 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid sender name")
			return
		}
		values[settings.KeySMTPFromName] = name
	}
	if req.SMTPSecurity != nil {
		sec := strings.ToLower(strings.TrimSpace(*req.SMTPSecurity))
		if sec != "none" && sec != "starttls" && sec != "tls" {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid SMTP security mode")
			return
		}
		values[settings.KeySMTPSecurity] = sec
	}
	if err := s.settings.Update(r.Context(), values); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not save settings")
		return
	}
	view, err := s.settingsView(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Settings saved, but reload failed")
		return
	}
	writeJSON(w, http.StatusOK, view)
}

type testEmailRequest struct {
	To string `json:"to"`
}

func (s *Server) handleTestEmail(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req testEmailRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	to := strings.TrimSpace(req.To)
	if to == "" || !validEmail(to) {
		writeError(w, http.StatusBadRequest, "invalid_email", "Enter a valid recipient email address")
		return
	}
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()
	smtpCfg, err := s.settings.EffectiveSMTP(ctx)
	if err != nil || !smtpCfg.Configured() {
		writeError(w, http.StatusBadRequest, "smtp_not_configured", "SMTP is not configured or disabled")
		return
	}
	subject := "Hugo Panel test email"
	text := fmt.Sprintf("This is a test message from Hugo Panel at %s.\nSMTP is working correctly.", time.Now().UTC().Format(time.RFC3339))
	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#f6f7f9;padding:28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111"><table role="presentation" width="100%%"><tr><td align="center"><table role="presentation" width="520" style="background:#fff;border:1px solid #e7e9ee;border-radius:16px;padding:32px"><tr><td><div style="font-size:13px;font-weight:700;letter-spacing:.08em;color:#6b7280">HUGO PANEL</div><h1 style="font-size:24px;margin:10px 0">SMTP is working</h1><p style="font-size:15px;color:#4b5563">This test message was sent at %s.</p></td></tr></table></td></tr></table></body></html>`, html.EscapeString(time.Now().UTC().Format(time.RFC3339)))
	if err := s.mailer.Send(ctx, smtpCfg, mailer.Message{To: to, Subject: subject, Text: text, HTML: htmlBody}); err != nil {
		s.logger.Error("send test email", "error", err)
		writeError(w, http.StatusBadGateway, "smtp_failed", "SMTP test failed: "+err.Error())
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleLocales(w http.ResponseWriter, r *http.Request, sess store.Session) {
	languages, err := s.i18n.Languages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load languages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"languages": languages})
}

type localeAddRequest struct {
	Code     string            `json:"code"`
	Name     string            `json:"name"`
	Messages map[string]string `json:"messages"`
}

func (s *Server) handleLocaleAdd(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req localeAddRequest
	if !decodeJSON(w, r, &req, 2*1024*1024) {
		return
	}
	if err := s.i18n.AddCustom(req.Code, req.Name, req.Messages); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_locale", err.Error())
		return
	}
	languages, _ := s.i18n.Languages()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "languages": languages})
}

func (s *Server) handleLocaleDelete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	code := strings.TrimSpace(r.URL.Query().Get("code"))
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Locale code is required")
		return
	}
	if err := s.i18n.DeleteCustom(code); err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Custom translation not found")
		return
	}
	languages, _ := s.i18n.Languages()
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "languages": languages})
}

func (s *Server) handleLocaleTemplate(w http.ResponseWriter, r *http.Request, sess store.Session) {
	messages, err := s.i18n.Template()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load translation template")
		return
	}
	writeJSON(w, http.StatusOK, messages)
}

func parseBoolSetting(v string) bool {
	b, _ := strconv.ParseBool(v)
	return b
}
