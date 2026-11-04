package hugo

import (
	"encoding/json"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"reflect"
	"regexp"
	"sort"
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
	ConfigFiles            []string               `json:"config_files,omitempty"`
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
	Extra                  map[string]any         `json:"extra"`
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
	Extra                  map[string]any         `json:"extra"`
	Menus                  map[string][]MenuEntry `json:"menus"`
}

var langCodeRe = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*$`)
var themeNameRe = regexp.MustCompile(`^[A-Za-z0-9._@/+-]+$`)

var rootConfigCandidates = []string{
	"hugo.toml", "hugo.yaml", "hugo.yml", "hugo.json",
	"config.toml", "config.yaml", "config.yml", "config.json",
}

// splitConfigCategories maps the category file name used in config/_default to
// the top-level key Hugo stores it under. Themes commonly split their settings
// into params.toml, menus.toml, markup.toml, module.toml and friends, so the
// panel has to read the whole directory rather than just hugo.toml.
var splitConfigCategories = map[string]string{
	"build":         "build",
	"caches":        "caches",
	"cascade":       "cascade",
	"contenttypes":  "contentTypes",
	"deployment":    "deployment",
	"frontmatter":   "frontmatter",
	"httpcache":     "httpcache",
	"imaging":       "imaging",
	"languages":     "languages",
	"markup":        "markup",
	"mediatypes":    "mediaTypes",
	"menus":         "menus",
	"minify":        "minify",
	"module":        "module",
	"outputformats": "outputFormats",
	"outputs":       "outputs",
	"page":          "page",
	"pagination":    "pagination",
	"params":        "params",
	"permalinks":    "permalinks",
	"privacy":       "privacy",
	"related":       "related",
	"roles":         "roles",
	"security":      "security",
	"segments":      "segments",
	"server":        "server",
	"services":      "services",
	"sitemap":       "sitemap",
	"taxonomies":    "taxonomies",
	"uglyurls":      "uglyURLs",
	"versions":      "versions",
}

// panelManagedConfigKeys are rendered by dedicated fields in the panel. Every
// other top-level key is exposed through the "extra" parameter editor.
var panelManagedConfigKeys = map[string]bool{
	"title":                  true,
	"baseurl":                true,
	"languagecode":           true,
	"defaultcontentlanguage": true,
	"theme":                  true,
	"params":                 true,
	"menus":                  true,
	"module":                 true,
}

func isManagedConfigKey(key string) bool {
	return panelManagedConfigKeys[strings.ToLower(strings.TrimSpace(key))]
}

func firstRootConfig(dir string) (string, string, bool) {
	for _, rel := range rootConfigCandidates {
		full := filepath.Join(dir, rel)
		if info, err := os.Stat(full); err == nil && !info.IsDir() {
			return rel, formatFromExt(full), true
		}
	}
	return "", "", false
}

func splitConfigFiles(dir string) []string {
	base := filepath.Join(dir, "config", "_default")
	var files []string
	_ = filepath.WalkDir(base, func(path string, entry fs.DirEntry, err error) error {
		if err != nil || entry == nil {
			return nil
		}
		if entry.IsDir() {
			if path != base && strings.HasPrefix(entry.Name(), ".") {
				return filepath.SkipDir
			}
			return nil
		}
		if strings.HasPrefix(entry.Name(), ".") {
			return nil
		}
		switch strings.ToLower(filepath.Ext(entry.Name())) {
		case ".toml", ".yaml", ".yml", ".json":
		default:
			return nil
		}
		stem := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if strings.Contains(stem, ".") {
			// Language- and environment-specific component files are left
			// untouched; merging them without their language context would
			// lose data.
			return nil
		}
		lower := strings.ToLower(stem)
		if lower != "hugo" && lower != "config" {
			if _, ok := splitConfigCategories[lower]; !ok {
				return nil
			}
		}
		if rel, err := filepath.Rel(dir, path); err == nil {
			files = append(files, filepath.ToSlash(rel))
		}
		return nil
	})
	sort.Strings(files)
	return files
}

func splitPrimary(files []string) string {
	for _, stem := range []string{"hugo", "config"} {
		for _, file := range files {
			base := strings.ToLower(filepath.Base(file))
			if strings.HasPrefix(base, stem+".") {
				return file
			}
		}
	}
	if len(files) > 0 {
		return files[0]
	}
	return ""
}

// splitCategory returns the top-level category represented by a split config
// file. Only plain category files (params.toml, menus.toml, ...) are merged;
// language-specific files are left untouched so the panel cannot accidentally
// collapse per-language settings into one file.
func splitCategory(rel string) (name, key string) {
	base := filepath.Base(rel)
	stem := strings.TrimSuffix(base, filepath.Ext(base))
	if strings.Contains(stem, ".") {
		return "", ""
	}
	lower := strings.ToLower(stem)
	if key, ok := splitConfigCategories[lower]; ok {
		return stem, key
	}
	return "", ""
}

func FindConfig(dir string) (string, string, bool) {
	if rel, format, ok := firstRootConfig(dir); ok {
		return rel, format, true
	}
	files := splitConfigFiles(dir)
	if len(files) > 0 {
		primary := splitPrimary(files)
		return primary, formatFromExt(primary), true
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
	return encodeConfigValue(m, format)
}

func encodeConfigValue(v any, format string) ([]byte, error) {
	switch format {
	case FormatYAML:
		return yaml.Marshal(v)
	case FormatJSON:
		return json.MarshalIndent(v, "", "  ")
	default:
		var b strings.Builder
		enc := toml.NewEncoder(&b)
		if err := enc.Encode(v); err != nil {
			return nil, fmt.Errorf("encode config TOML: %w", err)
		}
		return []byte(b.String()), nil
	}
}

type configSource struct {
	rel      string
	format   string
	category string // canonical top-level key, e.g. "params" or "mediaTypes"
	wrapper  string // file basename category, used when a non-map value is wrapped
	data     any    // root sources hold map[string]any; category sources hold the category value
}

type configDocument struct {
	sources []configSource
	primary int
	exists  bool
	merged  map[string]any
}

func (d *configDocument) contribution(src configSource) map[string]any {
	switch data := src.data.(type) {
	case map[string]any:
		if src.category != "" {
			return map[string]any{src.category: data}
		}
		return data
	default:
		if src.category != "" {
			return map[string]any{src.category: data}
		}
		return nil
	}
}

func (d *configDocument) targetFor(key string) int {
	for i := len(d.sources) - 1; i >= 0; i-- {
		if d.sources[i].category == key {
			return i
		}
	}
	for i := len(d.sources) - 1; i >= 0; i-- {
		src := d.sources[i]
		if src.category != "" {
			continue
		}
		if m, ok := src.data.(map[string]any); ok {
			if _, exists := m[key]; exists {
				return i
			}
		}
	}
	for i := len(d.sources) - 1; i >= 0; i-- {
		if d.sources[i].category == "" {
			return i
		}
	}
	return -1
}

func cloneConfigValue(v any) any {
	switch x := v.(type) {
	case map[string]any:
		out := make(map[string]any, len(x))
		for k, val := range x {
			out[k] = cloneConfigValue(val)
		}
		return out
	case []any:
		out := make([]any, len(x))
		for i, val := range x {
			out[i] = cloneConfigValue(val)
		}
		return out
	default:
		return v
	}
}

func cloneConfigMap(m map[string]any) map[string]any {
	out := make(map[string]any, len(m))
	for k, v := range m {
		out[k] = cloneConfigValue(v)
	}
	return out
}

func mergeConfigMap(dst, src map[string]any) {
	for key, value := range src {
		if srcMap, ok := value.(map[string]any); ok {
			if dstMap, ok := dst[key].(map[string]any); ok {
				mergeConfigMap(dstMap, srcMap)
				continue
			}
		}
		dst[key] = cloneConfigValue(value)
	}
}

func unwrapSplitCategory(raw map[string]any, category string) any {
	if len(raw) == 1 {
		for key, value := range raw {
			if strings.EqualFold(key, category) {
				return value
			}
		}
	}
	return raw
}

func (s *Site) readConfigDocument() (*configDocument, error) {
	if rel, format, ok := firstRootConfig(s.Dir); ok {
		data, err := os.ReadFile(filepath.Join(s.Dir, rel))
		if err != nil {
			return nil, err
		}
		raw, err := decodeConfig(data, format)
		if err != nil {
			return nil, err
		}
		return &configDocument{
			sources: []configSource{{rel: rel, format: format, data: raw}},
			primary: 0,
			exists:  true,
			merged:  cloneConfigMap(raw),
		}, nil
	}

	files := splitConfigFiles(s.Dir)
	if len(files) == 0 {
		return &configDocument{
			sources: []configSource{{rel: "hugo.toml", format: FormatTOML, data: map[string]any{}}},
			primary: 0,
			exists:  false,
			merged:  map[string]any{},
		}, nil
	}

	doc := &configDocument{sources: []configSource{}, exists: true, merged: map[string]any{}}
	for _, rel := range files {
		format := formatFromExt(rel)
		raw, err := os.ReadFile(filepath.Join(s.Dir, rel))
		if err != nil {
			return nil, err
		}
		decoded, err := decodeConfig(raw, format)
		if err != nil {
			return nil, err
		}
		src := configSource{rel: rel, format: format, data: decoded}
		if category, key := splitCategory(rel); category != "" {
			src.category = key
			src.wrapper = category
			src.data = unwrapSplitCategory(decoded, category)
		}
		doc.sources = append(doc.sources, src)
		mergeConfigMap(doc.merged, doc.contribution(src))
	}
	primary := splitPrimary(files)
	doc.primary = 0
	for i := range doc.sources {
		if doc.sources[i].rel == primary {
			doc.primary = i
			break
		}
	}
	return doc, nil
}

func (s *Site) writeConfigDocument(doc *configDocument, before map[string]any) error {
	changed := map[string]bool{}
	for key, value := range before {
		current, ok := doc.merged[key]
		if !ok || !reflect.DeepEqual(value, current) {
			changed[key] = true
		}
	}
	for key := range doc.merged {
		if _, ok := before[key]; !ok {
			changed[key] = true
		}
	}
	if len(changed) == 0 {
		return nil
	}

	dirty := map[int]bool{}
	for key := range changed {
		value, exists := doc.merged[key]
		if !exists {
			for i := range doc.sources {
				src := &doc.sources[i]
				if src.category == key {
					src.data = map[string]any{}
					dirty[i] = true
					continue
				}
				if src.category != "" {
					continue
				}
				if m, ok := src.data.(map[string]any); ok {
					if _, ok := m[key]; ok {
						delete(m, key)
						dirty[i] = true
					}
				}
			}
			continue
		}
		target := doc.targetFor(key)
		if target < 0 {
			rel := "hugo.toml"
			if len(doc.sources) > 0 && doc.sources[0].category != "" {
				rel = filepath.ToSlash(filepath.Join("config", "_default", "hugo.toml"))
			}
			doc.sources = append(doc.sources, configSource{rel: rel, format: FormatTOML, data: map[string]any{}})
			target = len(doc.sources) - 1
		}
		value = cloneConfigValue(value)
		src := &doc.sources[target]
		if src.category == key {
			src.data = value
			dirty[target] = true
		} else if m, ok := src.data.(map[string]any); ok {
			m[key] = value
			dirty[target] = true
		}
		for i := range doc.sources {
			if i == target {
				continue
			}
			other := &doc.sources[i]
			if other.category == key {
				other.data = map[string]any{}
				dirty[i] = true
				continue
			}
			if other.category != "" {
				continue
			}
			if m, ok := other.data.(map[string]any); ok {
				if _, exists := m[key]; exists {
					delete(m, key)
					dirty[i] = true
				}
			}
		}
	}

	for i := range doc.sources {
		if !dirty[i] {
			continue
		}
		src := doc.sources[i]
		var (
			data []byte
			err  error
		)
		if src.category != "" {
			value := src.data
			if value == nil {
				value = map[string]any{}
			}
			if _, ok := value.(map[string]any); !ok {
				wrapper := src.wrapper
				if wrapper == "" {
					wrapper = src.category
				}
				value = map[string]any{wrapper: value}
			}
			data, err = encodeConfigValue(value, src.format)
		} else {
			m, _ := src.data.(map[string]any)
			if m == nil {
				m = map[string]any{}
			}
			data, err = encodeConfig(m, src.format)
		}
		if err != nil {
			return err
		}
		full := filepath.Join(s.Dir, src.rel)
		if old, err := os.ReadFile(full); err == nil {
			s.backup(src.rel, old)
		}
		if err := atomicWrite(full, data, 0o644); err != nil {
			return err
		}
	}
	return nil
}

func (s *Site) LoadConfig() (SiteConfig, error) {
	doc, err := s.readConfigDocument()
	if err != nil {
		return SiteConfig{}, err
	}
	m := doc.merged
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
	extra := map[string]any{}
	for key, value := range m {
		if !isManagedConfigKey(key) {
			extra[key] = cloneConfigValue(value)
		}
	}
	primary := doc.sources[doc.primary].rel
	files := make([]string, 0, len(doc.sources))
	for _, src := range doc.sources {
		files = append(files, src.rel)
	}
	cfg := SiteConfig{
		ConfigPath:             filepath.ToSlash(primary),
		ConfigFiles:            files,
		Format:                 doc.sources[doc.primary].format,
		Exists:                 doc.exists,
		Initialized:            doc.exists,
		SiteTitle:              StringValue(m["title"]),
		BaseURL:                StringValue(m["baseURL"]),
		LanguageCode:           StringValue(m["languageCode"]),
		DefaultContentLanguage: StringValue(m["defaultContentLanguage"]),
		Theme:                  active,
		Themes:                 themes,
		Params:                 params,
		Extra:                  extra,
		Menus:                  parseMenus(m["menus"]),
		Modules:                moduleImports(m),
	}
	return cfg, nil
}

func (s *Site) SaveConfig(patch ConfigPatch) (SiteConfig, error) {
	doc, err := s.readConfigDocument()
	if err != nil {
		return SiteConfig{}, err
	}
	before := cloneConfigMap(doc.merged)
	m := doc.merged
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
	if patch.Extra != nil {
		for key := range m {
			if isManagedConfigKey(key) {
				continue
			}
			if _, keep := patch.Extra[key]; !keep {
				delete(m, key)
			}
		}
		for key, value := range patch.Extra {
			key = strings.TrimSpace(key)
			if key == "" {
				return SiteConfig{}, errf("invalid_request", "Configuration key must not be empty")
			}
			if isManagedConfigKey(key) {
				return SiteConfig{}, errf("invalid_request", "“"+key+"” is managed by the standard settings")
			}
			m[key] = normalizeValue(value)
		}
	}
	if patch.Menus != nil {
		m["menus"] = encodeMenus(patch.Menus)
	}
	if err := s.writeConfigDocument(doc, before); err != nil {
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
