package hugo

import (
	"encoding/json"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"time"

	"github.com/BurntSushi/toml"
	"gopkg.in/yaml.v3"
)

type MenuEntry struct {
	Name       string `json:"name"`
	PageRef    string `json:"page_ref,omitempty"`
	URL        string `json:"url,omitempty"`
	Weight     int    `json:"weight,omitempty"`
	Identifier string `json:"identifier,omitempty"`
	Parent     string `json:"parent,omitempty"`
	Pre        string `json:"pre,omitempty"`
	Post       string `json:"post,omitempty"`
}

type SiteConfig struct {
	ConfigPath             string                 `json:"config_path"`
	Format                 string                 `json:"format"`
	Exists                 bool                   `json:"exists"`
	Initialized            bool                   `json:"initialized"`
	SiteTitle              string                 `json:"site_title"`
	BaseURL                string                 `json:"base_url"`
	LanguageCode           string                 `json:"language_code"`
	DefaultContentLanguage string                 `json:"default_content_language"`
	Theme                  string                 `json:"theme"`
	Themes                 []string               `json:"themes"`
	Params                 map[string]any         `json:"params"`
	Menus                  map[string][]MenuEntry `json:"menus"`
	Modules                []ModuleImport         `json:"modules"`
}

type ConfigPatch struct {
	SiteTitle              *string                `json:"site_title"`
	BaseURL                *string                `json:"base_url"`
	LanguageCode           *string                `json:"language_code"`
	DefaultContentLanguage *string                `json:"default_content_language"`
	Theme                  *string                `json:"theme"`
	Params                 map[string]any         `json:"params"`
	Menus                  map[string][]MenuEntry `json:"menus"`
}

var langCodeRe = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*$`)
var themeNameRe = regexp.MustCompile(`^[A-Za-z0-9._@/+-]+$`)

var configCandidates = []string{
	"hugo.toml", "hugo.yaml", "hugo.yml", "hugo.json",
	"config.toml", "config.yaml", "config.yml", "config.json",
	filepath.Join("config", "_default", "hugo.toml"),
	filepath.Join("config", "_default", "hugo.yaml"),
	filepath.Join("config", "_default", "hugo.yml"),
	filepath.Join("config", "_default", "hugo.json"),
	filepath.Join("config", "_default", "config.toml"),
	filepath.Join("config", "_default", "config.yaml"),
	filepath.Join("config", "_default", "config.yml"),
	filepath.Join("config", "_default", "config.json"),
}

func FindConfig(dir string) (string, string, bool) {
	for _, rel := range configCandidates {
		full := filepath.Join(dir, rel)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return rel, formatFromExt(full), true
		}
	}
	return "hugo.toml", FormatTOML, false
}

func formatFromExt(path string) string {
	switch strings.ToLower(filepath.Ext(path)) {
	case ".yaml", ".yml":
		return FormatYAML
	case ".json":
		return FormatJSON
	default:
		return FormatTOML
	}
}

func decodeConfig(data []byte, format string) (map[string]any, error) {
	m := map[string]any{}
	switch format {
	case FormatYAML:
		if err := yaml.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse config YAML: %w", err)
		}
	case FormatJSON:
		if err := json.Unmarshal(data, &m); err != nil {
			return nil, fmt.Errorf("parse config JSON: %w", err)
		}
	default:
		if _, err := toml.Decode(string(data), &m); err != nil {
			return nil, fmt.Errorf("parse config TOML: %w", err)
		}
	}
	if m == nil {
		m = map[string]any{}
	}
	return normalizeMap(m), nil
}

func encodeConfig(m map[string]any, format string) ([]byte, error) {
	m = normalizeMap(m)
	switch format {
	case FormatYAML:
		return yaml.Marshal(m)
	case FormatJSON:
		return json.MarshalIndent(m, "", "  ")
	default:
		var b strings.Builder
		enc := toml.NewEncoder(&b)
		if err := enc.Encode(m); err != nil {
			return nil, fmt.Errorf("encode config TOML: %w", err)
		}
		return []byte(b.String()), nil
	}
}

func (s *Site) loadConfigMap() (rel, format string, m map[string]any, exists bool, err error) {
	rel, format, exists = FindConfig(s.Dir)
	if !exists {
		return rel, format, map[string]any{}, false, nil
	}
	data, err := os.ReadFile(filepath.Join(s.Dir, rel))
	if err != nil {
		return rel, format, nil, true, err
	}
	m, err = decodeConfig(data, format)
	return rel, format, m, true, err
}

func (s *Site) LoadConfig() (SiteConfig, error) {
	rel, format, m, exists, err := s.loadConfigMap()
	if err != nil {
		return SiteConfig{}, err
	}
	themes := themeNames(m["theme"])
	if themes == nil {
		themes = []string{}
	}
	active := ""
	if len(themes) > 0 {
		active = themes[0]
	}
	params := map[string]any{}
	if raw, ok := m["params"].(map[string]any); ok {
		params = raw
	}
	if params == nil {
		params = map[string]any{}
	}
	cfg := SiteConfig{
		ConfigPath:             filepath.ToSlash(rel),
		Format:                 format,
		Exists:                 exists,
		Initialized:            exists,
		SiteTitle:              StringValue(m["title"]),
		BaseURL:                StringValue(m["baseURL"]),
		LanguageCode:           StringValue(m["languageCode"]),
		DefaultContentLanguage: StringValue(m["defaultContentLanguage"]),
		Theme:                  active,
		Themes:                 themes,
		Params:                 params,
		Menus:                  parseMenus(m["menus"]),
		Modules:                moduleImports(m),
	}
	return cfg, nil
}

func (s *Site) SaveConfig(patch ConfigPatch) (SiteConfig, error) {
	rel, format, m, _, err := s.loadConfigMap()
	if err != nil {
		return SiteConfig{}, err
	}
	if patch.SiteTitle != nil {
		m["title"] = strings.TrimSpace(*patch.SiteTitle)
	}
	if patch.BaseURL != nil {
		v := strings.TrimSpace(*patch.BaseURL)
		if v != "" {
			u, err := url.Parse(v)
			if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
				return SiteConfig{}, errf("invalid_request", "Base URL must be an absolute http(s) URL")
			}
		}
		m["baseURL"] = v
	}
	if patch.LanguageCode != nil {
		v := strings.TrimSpace(*patch.LanguageCode)
		if v != "" && !langCodeRe.MatchString(v) {
			return SiteConfig{}, errf("invalid_request", "Invalid language code")
		}
		m["languageCode"] = v
	}
	if patch.DefaultContentLanguage != nil {
		v := strings.TrimSpace(*patch.DefaultContentLanguage)
		if v != "" && !langCodeRe.MatchString(v) {
			return SiteConfig{}, errf("invalid_request", "Invalid default content language")
		}
		m["defaultContentLanguage"] = v
	}
	if patch.Theme != nil {
		v := strings.TrimSpace(*patch.Theme)
		if v != "" {
			if strings.Contains(v, "..") || !themeNameRe.MatchString(v) {
				return SiteConfig{}, errf("invalid_request", "Invalid theme name")
			}
			m["theme"] = v
		} else {
			delete(m, "theme")
		}
	}
	if patch.Params != nil {
		m["params"] = normalizeMap(patch.Params)
	}
	if patch.Menus != nil {
		m["menus"] = encodeMenus(patch.Menus)
	}
	data, err := encodeConfig(m, format)
	if err != nil {
		return SiteConfig{}, err
	}
	full := filepath.Join(s.Dir, rel)
	if err := os.MkdirAll(filepath.Dir(full), 0o755); err != nil {
		return SiteConfig{}, err
	}
	if old, err := os.ReadFile(full); err == nil {
		s.backup(rel, old)
	}
	if err := atomicWrite(full, data, 0o644); err != nil {
		return SiteConfig{}, err
	}
	return s.LoadConfig()
}

func (s *Site) backup(rel string, data []byte) {
	if s.DataDir == "" || len(data) == 0 {
		return
	}
	dir := filepath.Join(s.DataDir, "backups")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return
	}
	name := fmt.Sprintf("%s-%s-%s", time.Now().UTC().Format("20060102-150405.000"), strings.ReplaceAll(rel, string(filepath.Separator), "_"), "bak")
	_ = os.WriteFile(filepath.Join(dir, name), data, 0o600)
}

func themeNames(v any) []string {
	switch x := v.(type) {
	case string:
		if strings.TrimSpace(x) == "" {
			return nil
		}
		return []string{strings.TrimSpace(x)}
	case []string:
		return append([]string(nil), x...)
	case []any:
		var out []string
		for _, item := range x {
			if s := strings.TrimSpace(StringValue(item)); s != "" {
				out = append(out, s)
			}
		}
		return out
	default:
		return nil
	}
}

func parseMenus(v any) map[string][]MenuEntry {
	out := map[string][]MenuEntry{}
	raw, ok := v.(map[string]any)
	if !ok {
		return out
	}
	for name, entries := range raw {
		list := AnySlice(entries)
		for _, entry := range list {
			mm, ok := entry.(map[string]any)
			if !ok {
				continue
			}
			out[name] = append(out[name], MenuEntry{
				Name:       StringValue(mm["name"]),
				PageRef:    StringValue(mm["pageRef"]),
				URL:        StringValue(mm["url"]),
				Weight:     IntValue(mm["weight"]),
				Identifier: StringValue(mm["identifier"]),
				Parent:     StringValue(mm["parent"]),
				Pre:        StringValue(mm["pre"]),
				Post:       StringValue(mm["post"]),
			})
		}
		if _, ok := out[name]; !ok {
			out[name] = []MenuEntry{}
		}
	}
	return out
}

func encodeMenus(menus map[string][]MenuEntry) map[string]any {
	out := map[string]any{}
	for name, entries := range menus {
		name = strings.TrimSpace(name)
		if name == "" {
			continue
		}
		list := make([]any, 0, len(entries))
		for _, e := range entries {
			item := map[string]any{}
			if strings.TrimSpace(e.Name) != "" {
				item["name"] = strings.TrimSpace(e.Name)
			}
			if strings.TrimSpace(e.PageRef) != "" {
				item["pageRef"] = strings.TrimSpace(e.PageRef)
			}
			if strings.TrimSpace(e.URL) != "" {
				item["url"] = strings.TrimSpace(e.URL)
			}
			if e.Weight != 0 {
				item["weight"] = e.Weight
			}
			if strings.TrimSpace(e.Identifier) != "" {
				item["identifier"] = strings.TrimSpace(e.Identifier)
			}
			if strings.TrimSpace(e.Parent) != "" {
				item["parent"] = strings.TrimSpace(e.Parent)
			}
			if strings.TrimSpace(e.Pre) != "" {
				item["pre"] = strings.TrimSpace(e.Pre)
			}
			if strings.TrimSpace(e.Post) != "" {
				item["post"] = strings.TrimSpace(e.Post)
			}
			if len(item) > 0 {
				list = append(list, item)
			}
		}
		out[name] = list
	}
	return out
}

func (s *Site) InitSite(title, baseURL, language string) error {
	if info, err := os.Stat(s.Dir); err == nil && !info.IsDir() {
		return errf("invalid_path", "Hugo path is not a directory")
	}
	for _, sub := range []string{"content", "static", "themes", "layouts", "archetypes", "data", "assets"} {
		if err := os.MkdirAll(filepath.Join(s.Dir, sub), 0o755); err != nil {
			return err
		}
	}
	if _, _, exists := FindConfig(s.Dir); exists {
		return errf("file_exists", "A Hugo configuration already exists")
	}
	if strings.TrimSpace(title) == "" {
		title = "My Hugo Site"
	}
	if strings.TrimSpace(language) == "" {
		language = "en"
	}
	if baseURL != "" {
		u, err := url.Parse(baseURL)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errf("invalid_request", "Invalid base URL")
		}
	}
	m := map[string]any{
		"baseURL":      baseURL,
		"title":        title,
		"languageCode": language,
		"theme":        "",
	}
	delete(m, "theme")
	b, err := encodeConfig(m, FormatTOML)
	if err != nil {
		return err
	}
	if err := atomicWrite(filepath.Join(s.Dir, "hugo.toml"), b, 0o644); err != nil {
		return err
	}
	return nil
}

func (s *Site) ConfigExists() bool {
	_, _, exists := FindConfig(s.Dir)
	return exists
}
