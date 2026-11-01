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

	"github.com/Kaiyuan/hugo-panel/internal/deploy"
	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
	"github.com/Kaiyuan/hugo-panel/internal/translate"
)

type settingsView struct {
	HugoDir         string          `json:"hugo_dir"`
	HugoBinary      string          `json:"hugo_binary"`
	PublicURL       string          `json:"public_url"`
	DefaultLanguage string          `json:"default_language"`
	DefaultTheme    string          `json:"default_theme"`
	Notifications   bool            `json:"notifications_enabled"`
	BuildOnSave     bool            `json:"build_on_save"`
	SMTP            smtpView        `json:"smtp"`
	Translation     translationView `json:"translation"`
	AI              aiView          `json:"ai"`
	Deploy          deployView      `json:"deploy"`
	EnvOverrides    []string        `json:"env_overrides"`
	SetupCompleted  string          `json:"setup_completed_at,omitempty"`
}

type translationView struct {
	Provider  string `json:"provider"`
	Endpoint  string `json:"endpoint"`
	APIKeySet bool   `json:"api_key_set"`
}

type aiView struct {
	Enabled   bool   `json:"enabled"`
	BaseURL   string `json:"base_url"`
	Model     string `json:"model"`
	APIKeySet bool   `json:"api_key_set"`
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

type deployView struct {
	Enabled       bool   `json:"enabled"`
	Host          string `json:"host"`
	Port          int    `json:"port"`
	User          string `json:"user"`
	Path          string `json:"path"`
	Source        string `json:"source"`
	Delete        bool   `json:"delete"`
	StrictHostKey bool   `json:"strict_host_key"`
	RsyncPath     string `json:"rsync_path"`
	SSHKeySet     bool   `json:"ssh_key_set"`
	KnownHostsSet bool   `json:"known_hosts_set"`
	Configured    bool   `json:"configured"`
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
	translateCfg, err := s.settings.Translation(ctx)
	if err != nil {
		return settingsView{}, err
	}
	aiCfg, err := s.settings.AI(ctx)
	if err != nil {
		return settingsView{}, err
	}
	deployCfg, err := s.settings.Deploy(ctx, s.settings.HugoDir(ctx))
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
		BuildOnSave:     parseBoolSetting(all[settings.KeyBuildOnSave]),
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
		Translation: translationView{
			Provider:  translateCfg.Provider,
			Endpoint:  translateCfg.Endpoint,
			APIKeySet: translateCfg.APIKey != "",
		},
		AI: aiView{
			Enabled:   aiCfg.Enabled,
			BaseURL:   aiCfg.BaseURL,
			Model:     aiCfg.Model,
			APIKeySet: aiCfg.APIKey != "",
		},
		Deploy: deployView{
			Enabled:       deployCfg.Enabled,
			Host:          deployCfg.Host,
			Port:          deployCfg.Port,
			User:          deployCfg.User,
			Path:          deployCfg.Path,
			Source:        deploySource(all),
			Delete:        deployCfg.Delete,
			StrictHostKey: deployCfg.StrictHostKey,
			RsyncPath:     deployCfg.RsyncPath,
			SSHKeySet:     deployCfg.SSHKey != "",
			KnownHostsSet: deployCfg.KnownHosts != "",
			Configured:    deployCfg.Validate() == nil,
		},
	}, nil
}

func deploySource(all map[string]string) string {
	if v := strings.TrimSpace(all[settings.KeyDeploySource]); v != "" {
		return v
	}
	return "public"
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
	BuildOnSave       *bool   `json:"build_on_save"`
	SMTPEnabled       *bool   `json:"smtp_enabled"`
	SMTPHost          *string `json:"smtp_host"`
	SMTPPort          *int    `json:"smtp_port"`
	SMTPUsername      *string `json:"smtp_username"`
	SMTPPassword      *string `json:"smtp_password"`
	ClearSMTPPassword bool    `json:"clear_smtp_password"`
	SMTPFrom          *string `json:"smtp_from"`
	SMTPFromName      *string `json:"smtp_from_name"`
	SMTPSecurity      *string `json:"smtp_security"`
	TranslateProvider *string `json:"translate_provider"`
	TranslateEndpoint *string `json:"translate_endpoint"`
	TranslateAPIKey   *string `json:"translate_api_key"`
	ClearTranslateKey bool    `json:"clear_translate_api_key"`
	AIEnabled         *bool   `json:"ai_enabled"`
	AIBaseURL         *string `json:"ai_base_url"`
	AIModel           *string `json:"ai_model"`
	AIAPIKey          *string `json:"ai_api_key"`
	ClearAIKey        bool    `json:"clear_ai_api_key"`
	DeployEnabled     *bool   `json:"deploy_enabled"`
	DeployHost        *string `json:"deploy_host"`
	DeployPort        *int    `json:"deploy_port"`
	DeployUser        *string `json:"deploy_user"`
	DeployPath        *string `json:"deploy_path"`
	DeploySource      *string `json:"deploy_source"`
	DeployDelete      *bool   `json:"deploy_delete"`
	DeployStrictHost  *bool   `json:"deploy_strict_host_key"`
	DeployRsyncPath   *string `json:"deploy_rsync_path"`
	DeploySSHKey      *string `json:"deploy_ssh_key"`
	ClearDeploySSHKey bool    `json:"clear_deploy_ssh_key"`
	DeployKnownHosts  *string `json:"deploy_known_hosts"`
	ClearDeployKnown  bool    `json:"clear_deploy_known_hosts"`
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
	if req.BuildOnSave != nil {
		values[settings.KeyBuildOnSave] = strconv.FormatBool(*req.BuildOnSave)
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
	if req.TranslateProvider != nil {
		provider := strings.ToLower(strings.TrimSpace(*req.TranslateProvider))
		if provider != "" && provider != translate.ProviderLibreTranslate && provider != translate.ProviderDeepL {
			writeError(w, http.StatusBadRequest, "invalid_request", "Unsupported translation provider")
			return
		}
		values[settings.KeyTranslateProvider] = provider
	}
	if req.TranslateEndpoint != nil {
		endpoint := strings.TrimSpace(*req.TranslateEndpoint)
		if endpoint != "" {
			u, err := url.Parse(endpoint)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "Translation endpoint must be an absolute http(s) URL")
				return
			}
		}
		values[settings.KeyTranslateEndpoint] = endpoint
	}
	if req.ClearTranslateKey {
		values[settings.KeyTranslateAPIKey] = ""
	} else if req.TranslateAPIKey != nil && *req.TranslateAPIKey != "" {
		if strings.ContainsAny(*req.TranslateAPIKey, "\x00\r\n") {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid translation API key")
			return
		}
		values[settings.KeyTranslateAPIKey] = strings.TrimSpace(*req.TranslateAPIKey)
	}
	if req.AIEnabled != nil {
		values[settings.KeyAIEnabled] = strconv.FormatBool(*req.AIEnabled)
	}
	if req.AIBaseURL != nil {
		baseURL := strings.TrimRight(strings.TrimSpace(*req.AIBaseURL), "/")
		if baseURL != "" {
			u, err := url.Parse(baseURL)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				writeError(w, http.StatusBadRequest, "invalid_request", "AI base URL must be an absolute http(s) URL")
				return
			}
		}
		values[settings.KeyAIBaseURL] = baseURL
	}
	if req.AIModel != nil {
		model := strings.TrimSpace(*req.AIModel)
		if strings.ContainsAny(model, "\x00\r\n") || len([]rune(model)) > 120 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid AI model name")
			return
		}
		values[settings.KeyAIModel] = model
	}
	if req.ClearAIKey {
		values[settings.KeyAIAPIKey] = ""
	} else if req.AIAPIKey != nil && *req.AIAPIKey != "" {
		if strings.ContainsAny(*req.AIAPIKey, "\x00\r\n") || len([]rune(*req.AIAPIKey)) > 500 {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid AI API key")
			return
		}
		values[settings.KeyAIAPIKey] = strings.TrimSpace(*req.AIAPIKey)
	}
	if req.DeployEnabled != nil {
		values[settings.KeyDeployEnabled] = strconv.FormatBool(*req.DeployEnabled)
	}
	if req.DeployHost != nil {
		host := strings.TrimSpace(*req.DeployHost)
		if host != "" && !deploy.ValidHost(host) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid remote host")
			return
		}
		values[settings.KeyDeployHost] = host
	}
	if req.DeployPort != nil {
		if *req.DeployPort < 1 || *req.DeployPort > 65535 {
			writeError(w, http.StatusBadRequest, "invalid_request", "SSH port must be between 1 and 65535")
			return
		}
		values[settings.KeyDeployPort] = strconv.Itoa(*req.DeployPort)
	}
	if req.DeployUser != nil {
		user := strings.TrimSpace(*req.DeployUser)
		if user != "" && !deploy.ValidUser(user) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid SSH user")
			return
		}
		values[settings.KeyDeployUser] = user
	}
	if req.DeployPath != nil {
		path := strings.TrimSpace(*req.DeployPath)
		if path != "" && !deploy.ValidRemotePath(path) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid remote directory")
			return
		}
		values[settings.KeyDeployPath] = path
	}
	if req.DeploySource != nil {
		source := strings.TrimSpace(*req.DeploySource)
		if source != "" && !validDeploySource(source) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid local source directory")
			return
		}
		values[settings.KeyDeploySource] = source
	}
	if req.DeployDelete != nil {
		values[settings.KeyDeployDelete] = strconv.FormatBool(*req.DeployDelete)
	}
	if req.DeployStrictHost != nil {
		values[settings.KeyDeployStrictHostKey] = strconv.FormatBool(*req.DeployStrictHost)
	}
	if req.DeployRsyncPath != nil {
		rsyncPath := strings.TrimSpace(*req.DeployRsyncPath)
		if rsyncPath != "" && (len(rsyncPath) > 500 || strings.ContainsAny(rsyncPath, "\x00\r\n\t ")) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid remote rsync path")
			return
		}
		values[settings.KeyDeployRsyncPath] = rsyncPath
	}
	if req.ClearDeploySSHKey {
		values[settings.KeyDeploySSHKey] = ""
	} else if req.DeploySSHKey != nil && strings.TrimSpace(*req.DeploySSHKey) != "" {
		if !deploy.ValidPrivateKey(*req.DeploySSHKey) {
			writeError(w, http.StatusBadRequest, "invalid_request", "The SSH private key does not look valid")
			return
		}
		values[settings.KeyDeploySSHKey] = *req.DeploySSHKey
	}
	if req.ClearDeployKnown {
		values[settings.KeyDeployKnownHosts] = ""
	} else if req.DeployKnownHosts != nil && strings.TrimSpace(*req.DeployKnownHosts) != "" {
		if !deploy.ValidKnownHosts(*req.DeployKnownHosts) {
			writeError(w, http.StatusBadRequest, "invalid_request", "Invalid known_hosts content")
			return
		}
		values[settings.KeyDeployKnownHosts] = *req.DeployKnownHosts
	}
	if len(values) > 0 {
		all, err := s.settings.All(r.Context())
		if err != nil {
			writeError(w, http.StatusInternalServerError, "server_error", "Could not load settings")
			return
		}
		merged := make(map[string]string, len(all)+len(values))
		for k, v := range all {
			merged[k] = v
		}
		for k, v := range values {
			merged[k] = v
		}
		finalDeploy := settings.DeployConfig(merged, s.settings.HugoDir(r.Context()))
		if finalDeploy.Enabled {
			if err := finalDeploy.Validate(); err != nil {
				writeError(w, http.StatusBadRequest, "invalid_request", "Deployment is enabled but incomplete: "+err.Error())
				return
			}
		}
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

func validDeploySource(source string) bool {
	if !deploy.ValidSource(source) {
		return false
	}
	if filepath.IsAbs(source) {
		return true
	}
	clean := filepath.Clean(source)
	return clean != ".." && !strings.HasPrefix(clean, ".."+string(filepath.Separator))
}
