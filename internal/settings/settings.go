package settings

import (
	"context"
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"github.com/Kaiyuan/hugo-panel/internal/ai"
	"github.com/Kaiyuan/hugo-panel/internal/config"
	"github.com/Kaiyuan/hugo-panel/internal/deploy"
	"github.com/Kaiyuan/hugo-panel/internal/store"
	"github.com/Kaiyuan/hugo-panel/internal/translate"
)

const (
	KeyDefaultLanguage   = "default_language"
	KeyDefaultTheme      = "default_theme"
	KeyHugoDir           = "hugo_dir"
	KeyHugoBinary        = "hugo_binary"
	KeyPublicURL         = "public_url"
	KeySMTPEnabled       = "smtp_enabled"
	KeySMTPHost          = "smtp_host"
	KeySMTPPort          = "smtp_port"
	KeySMTPUsername      = "smtp_username"
	KeySMTPPassword      = "smtp_password"
	KeySMTPFrom          = "smtp_from"
	KeySMTPFromName      = "smtp_from_name"
	KeySMTPSecurity      = "smtp_security"
	KeyTranslateProvider = "translate_provider"
	KeyTranslateEndpoint = "translate_endpoint"
	KeyTranslateAPIKey   = "translate_api_key"
	KeyAIEnabled         = "ai_enabled"
	KeyAIBaseURL         = "ai_base_url"
	KeyAIModel           = "ai_model"
	KeyAIAPIKey          = "ai_api_key"
	KeyBuildOnSave       = "build_on_save"
	KeyNotifications     = "notifications_enabled"
	KeyLastBuild         = "last_build"
	KeySetupCompletedAt  = "setup_completed_at"

	KeyDeployEnabled       = "deploy_enabled"
	KeyDeployHost          = "deploy_host"
	KeyDeployPort          = "deploy_port"
	KeyDeployUser          = "deploy_user"
	KeyDeployPath          = "deploy_path"
	KeyDeploySource        = "deploy_source"
	KeyDeployDelete        = "deploy_delete"
	KeyDeployStrictHostKey = "deploy_strict_host_key"
	KeyDeploySSHKey        = "deploy_ssh_key"
	KeyDeploySSHKeyAddedAt = "deploy_ssh_key_added_at"
	KeyDeployKnownHosts    = "deploy_known_hosts"
	KeyDeployRsyncPath     = "deploy_rsync_path"
	KeyLastDeploy          = "last_deploy"
)

type Manager struct {
	store *store.Store
	cfg   *config.Config
}

type SMTP struct {
	Enabled   bool
	Host      string
	Port      int
	Username  string
	Password  string
	From      string
	FromName  string
	Security  string // none, starttls, tls
	Overrides []string
}

func New(st *store.Store, cfg *config.Config) *Manager {
	return &Manager{store: st, cfg: cfg}
}

func (m *Manager) defaults() map[string]string {
	hugoDir, err := filepath.Abs(m.cfg.DefaultHugoDir)
	if err != nil {
		hugoDir = m.cfg.DefaultHugoDir
	}
	return map[string]string{
		KeyDefaultLanguage:     "en",
		KeyDefaultTheme:        "system",
		KeyHugoDir:             hugoDir,
		KeyHugoBinary:          m.cfg.DefaultHugoBin,
		KeyPublicURL:           "",
		KeySMTPEnabled:         "false",
		KeySMTPHost:            "",
		KeySMTPPort:            "587",
		KeySMTPUsername:        "",
		KeySMTPPassword:        "",
		KeySMTPFrom:            "",
		KeySMTPFromName:        "Hugo Panel",
		KeySMTPSecurity:        "starttls",
		KeyTranslateProvider:   "",
		KeyTranslateEndpoint:   "",
		KeyTranslateAPIKey:     "",
		KeyAIEnabled:           "false",
		KeyAIBaseURL:           ai.DefaultBaseURL,
		KeyAIModel:             ai.DefaultModel,
		KeyAIAPIKey:            "",
		KeyBuildOnSave:         "false",
		KeyNotifications:       "true",
		KeyDeployEnabled:       "false",
		KeyDeployHost:          "",
		KeyDeployPort:          "22",
		KeyDeployUser:          "",
		KeyDeployPath:          "",
		KeyDeploySource:        "public",
		KeyDeployDelete:        "true",
		KeyDeployStrictHostKey: "true",
		KeyDeploySSHKey:        "",
		KeyDeploySSHKeyAddedAt: "",
		KeyDeployKnownHosts:    "",
		KeyDeployRsyncPath:     "",
	}
}

func (m *Manager) EnsureDefaults(ctx context.Context) error {
	for k, v := range m.defaults() {
		if err := m.store.EnsureSetting(ctx, k, v); err != nil {
			return fmt.Errorf("seed setting %s: %w", k, err)
		}
	}
	return nil
}

func (m *Manager) All(ctx context.Context) (map[string]string, error) {
	values, err := m.store.Settings(ctx)
	if err != nil {
		return nil, err
	}
	for k, v := range m.defaults() {
		if _, ok := values[k]; !ok {
			values[k] = v
		}
	}
	return values, nil
}

func (m *Manager) Get(ctx context.Context, key string) (string, error) {
	if v, ok, err := m.store.GetSetting(ctx, key); err != nil {
		return "", err
	} else if ok {
		return v, nil
	}
	return m.defaults()[key], nil
}

func (m *Manager) Update(ctx context.Context, values map[string]string) error {
	return m.store.SetSettings(ctx, values)
}

func (m *Manager) Bool(ctx context.Context, key string) bool {
	v, _ := m.Get(ctx, key)
	b, _ := strconv.ParseBool(v)
	return b
}

func (m *Manager) HugoDir(ctx context.Context) string {
	v, _ := m.Get(ctx, KeyHugoDir)
	if v == "" {
		return m.cfg.DefaultHugoDir
	}
	return v
}

func (m *Manager) HugoBinary(ctx context.Context) string {
	v, _ := m.Get(ctx, KeyHugoBinary)
	if v == "" {
		return m.cfg.DefaultHugoBin
	}
	return v
}

func (m *Manager) PublicURL(ctx context.Context) string {
	if m.cfg.PublicURL != "" {
		return m.cfg.PublicURL
	}
	v, _ := m.Get(ctx, KeyPublicURL)
	return strings.TrimRight(strings.TrimSpace(v), "/")
}

func (m *Manager) PublicURLFromEnv() bool { return m.cfg.PublicURL != "" }

// Translation returns the effective title-translation configuration. The API
// key is only used for outbound requests and is never sent to clients.
func (m *Manager) Translation(ctx context.Context) (translate.Config, error) {
	all, err := m.All(ctx)
	if err != nil {
		return translate.Config{}, err
	}
	return translate.Config{
		Provider: all[KeyTranslateProvider],
		Endpoint: all[KeyTranslateEndpoint],
		APIKey:   all[KeyTranslateAPIKey],
	}.Normalized(), nil
}

// AI returns the effective AI provider configuration. The API key is only
// used for outbound requests and is never sent to clients.
func (m *Manager) AI(ctx context.Context) (ai.Config, error) {
	all, err := m.All(ctx)
	if err != nil {
		return ai.Config{}, err
	}
	return ai.Config{
		Enabled: all[KeyAIEnabled] == "true",
		BaseURL: all[KeyAIBaseURL],
		Model:   all[KeyAIModel],
		APIKey:  all[KeyAIAPIKey],
	}.Normalized(), nil
}

// Deploy returns the effective rsync deployment configuration. The private key
// and known_hosts content are only used by the deployer and are never exposed
// through the settings API.
func (m *Manager) Deploy(ctx context.Context, siteDir string) (deploy.Config, error) {
	all, err := m.All(ctx)
	if err != nil {
		return deploy.Config{}, err
	}
	return DeployConfig(all, siteDir), nil
}

// DeployConfig builds a deployment configuration from raw setting values. It is
// also used by the settings handler to validate a patch before saving it.
func DeployConfig(values map[string]string, siteDir string) deploy.Config {
	port, _ := strconv.Atoi(values[KeyDeployPort])
	if port == 0 {
		port = 22
	}
	source := strings.TrimSpace(values[KeyDeploySource])
	if source == "" {
		source = "public"
	}
	if filepath.IsAbs(source) {
		source = filepath.Clean(source)
	} else if siteDir != "" {
		source = filepath.Join(siteDir, source)
	}
	return deploy.Config{
		Enabled:       parseBool(values[KeyDeployEnabled]),
		Host:          strings.TrimSpace(values[KeyDeployHost]),
		Port:          port,
		User:          strings.TrimSpace(values[KeyDeployUser]),
		Path:          strings.TrimSpace(values[KeyDeployPath]),
		Source:        source,
		Delete:        parseBool(values[KeyDeployDelete]),
		StrictHostKey: parseBool(values[KeyDeployStrictHostKey]),
		SSHKey:        values[KeyDeploySSHKey],
		KnownHosts:    values[KeyDeployKnownHosts],
		RsyncPath:     strings.TrimSpace(values[KeyDeployRsyncPath]),
	}.Normalized()
}

func (m *Manager) SMTPOverrides() []string {
	var keys []string
	e := m.cfg.SMTP
	if e.Enabled != nil {
		keys = append(keys, KeySMTPEnabled)
	}
	if e.Host != "" {
		keys = append(keys, KeySMTPHost)
	}
	if e.Port != "" {
		keys = append(keys, KeySMTPPort)
	}
	if e.Username != "" {
		keys = append(keys, KeySMTPUsername)
	}
	if e.Password != "" {
		keys = append(keys, KeySMTPPassword)
	}
	if e.From != "" {
		keys = append(keys, KeySMTPFrom)
	}
	if e.FromName != "" {
		keys = append(keys, KeySMTPFromName)
	}
	if e.Security != "" {
		keys = append(keys, KeySMTPSecurity)
	}
	return keys
}

func (m *Manager) EffectiveSMTP(ctx context.Context) (SMTP, error) {
	all, err := m.All(ctx)
	if err != nil {
		return SMTP{}, err
	}
	out := SMTP{
		Enabled:   parseBool(all[KeySMTPEnabled]),
		Host:      all[KeySMTPHost],
		Username:  all[KeySMTPUsername],
		Password:  all[KeySMTPPassword],
		From:      all[KeySMTPFrom],
		FromName:  all[KeySMTPFromName],
		Security:  strings.ToLower(all[KeySMTPSecurity]),
		Overrides: m.SMTPOverrides(),
	}
	out.Port, _ = strconv.Atoi(all[KeySMTPPort])
	if out.Port == 0 {
		out.Port = 587
	}
	e := m.cfg.SMTP
	if e.Enabled != nil {
		out.Enabled = *e.Enabled
	}
	if e.Host != "" {
		out.Host = e.Host
	}
	if e.Port != "" {
		out.Port, _ = strconv.Atoi(e.Port)
	}
	if e.Username != "" {
		out.Username = e.Username
	}
	if e.Password != "" {
		out.Password = e.Password
	}
	if e.From != "" {
		out.From = e.From
	}
	if e.FromName != "" {
		out.FromName = e.FromName
	}
	if e.Security != "" {
		out.Security = strings.ToLower(e.Security)
	}
	if out.From == "" {
		out.From = out.Username
	}
	if out.Security == "" {
		out.Security = "starttls"
	}
	return out, nil
}

func (s SMTP) Configured() bool {
	return s.Enabled && s.Host != "" && s.From != ""
}

func (s SMTP) IsOverridden(key string) bool {
	for _, k := range s.Overrides {
		if k == key {
			return true
		}
	}
	return false
}

func parseBool(v string) bool {
	b, _ := strconv.ParseBool(v)
	return b
}
