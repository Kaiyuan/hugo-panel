package i18n

import (
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"net/url"
	"os"
	"path/filepath"
	"regexp"
	"sort"
	"strings"
)

type Language struct {
	Code       string `json:"code"`
	Name       string `json:"name"`
	NativeName string `json:"nativeName"`
	Builtin    bool   `json:"builtin"`
}

type Manager struct {
	embedded  fs.FS
	customDir string
}

var codeRe = regexp.MustCompile(`^[A-Za-z]{2,3}([_-][A-Za-z0-9]{2,8})*$`)

var builtinOrder = []string{"en", "zh-CN", "zh-TW", "fr", "it", "es", "ja", "ko", "de"}

func New(embedded fs.FS, dataDir string) *Manager {
	return &Manager{embedded: embedded, customDir: filepath.Join(dataDir, "locales")}
}

func (m *Manager) Languages() ([]Language, error) {
	seen := map[string]Language{}
	entries, err := fs.ReadDir(m.embedded, ".")
	if err != nil {
		return nil, err
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		code := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		lang, err := m.readMeta(m.embedded, entry.Name(), code, true)
		if err != nil {
			continue
		}
		seen[code] = lang
	}
	customEntries, err := os.ReadDir(m.customDir)
	if err == nil {
		for _, entry := range customEntries {
			if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
				continue
			}
			code := normalizeCode(strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name())))
			if !codeRe.MatchString(code) {
				continue
			}
			lang, err := m.readMeta(os.DirFS(m.customDir), entry.Name(), code, false)
			if err != nil {
				continue
			}
			seen[code] = lang
		}
	}
	var out []Language
	for _, code := range builtinOrder {
		if lang, ok := seen[code]; ok {
			out = append(out, lang)
			delete(seen, code)
		}
	}
	var rest []Language
	for _, lang := range seen {
		rest = append(rest, lang)
	}
	sort.Slice(rest, func(i, j int) bool {
		return strings.ToLower(rest[i].NativeName) < strings.ToLower(rest[j].NativeName)
	})
	out = append(out, rest...)
	if len(out) == 0 {
		return nil, fmt.Errorf("no locales found")
	}
	return out, nil
}

func (m *Manager) readMeta(fsys fs.FS, filename, code string, builtin bool) (Language, error) {
	data, err := fs.ReadFile(fsys, filename)
	if err != nil {
		return Language{}, err
	}
	var raw map[string]any
	if err := json.Unmarshal(data, &raw); err != nil {
		return Language{}, err
	}
	lang := Language{Code: code, Name: code, NativeName: code, Builtin: builtin}
	if v, ok := raw["_name"].(string); ok && strings.TrimSpace(v) != "" {
		lang.Name = v
	}
	if v, ok := raw["_native_name"].(string); ok && strings.TrimSpace(v) != "" {
		lang.NativeName = v
	}
	return lang, nil
}

func (m *Manager) Messages(code string) (map[string]string, error) {
	code = normalizeCode(code)
	if !codeRe.MatchString(code) {
		return nil, fmt.Errorf("invalid locale code")
	}
	en, err := m.readMessagesFS(m.embedded, "en.json")
	if err != nil {
		return nil, err
	}
	out := en
	if code != "en" {
		if data, err := fs.ReadFile(m.embedded, code+".json"); err == nil {
			var messages map[string]string
			if json.Unmarshal(data, &messages) == nil {
				merge(out, messages)
			}
		}
	}
	if customPath := m.findCustom(code); customPath != "" {
		if data, err := os.ReadFile(customPath); err == nil {
			var messages map[string]string
			if json.Unmarshal(data, &messages) == nil {
				merge(out, messages)
			}
		}
	}
	return out, nil
}

func (m *Manager) readMessagesFS(fsys fs.FS, filename string) (map[string]string, error) {
	data, err := fs.ReadFile(fsys, filename)
	if err != nil {
		return nil, err
	}
	var messages map[string]string
	if err := json.Unmarshal(data, &messages); err != nil {
		return nil, err
	}
	return messages, nil
}

func merge(dst, src map[string]string) {
	for k, v := range src {
		if strings.HasPrefix(k, "_") || v == "" {
			continue
		}
		dst[k] = v
	}
}

func (m *Manager) AddCustom(code, name string, messages map[string]string) error {
	code = normalizeCode(code)
	if !codeRe.MatchString(code) {
		return fmt.Errorf("invalid locale code")
	}
	name = strings.TrimSpace(name)
	if name == "" || len([]rune(name)) > 80 {
		return fmt.Errorf("invalid locale name")
	}
	if len(messages) > 8000 {
		return fmt.Errorf("too many translation entries")
	}
	clean := map[string]any{"_name": name, "_native_name": name}
	for k, v := range messages {
		k = strings.TrimSpace(k)
		if k == "" || strings.HasPrefix(k, "_") || len(k) > 200 {
			return fmt.Errorf("invalid translation key %q", k)
		}
		if len([]rune(v)) > 5000 {
			return fmt.Errorf("translation value for %s is too long", k)
		}
		clean[k] = v
	}
	if len(clean) <= 2 {
		return fmt.Errorf("translation is empty")
	}
	if err := os.MkdirAll(m.customDir, 0o750); err != nil {
		return err
	}
	data, err := json.MarshalIndent(clean, "", "  ")
	if err != nil {
		return err
	}
	data = append(data, '\n')
	return atomicWrite(filepath.Join(m.customDir, code+".json"), data)
}

func (m *Manager) DeleteCustom(code string) error {
	code = normalizeCode(code)
	if !codeRe.MatchString(code) {
		return fmt.Errorf("invalid locale code")
	}
	path := m.findCustom(code)
	if path == "" {
		return fmt.Errorf("custom locale not found")
	}
	if err := os.Remove(path); errors.Is(err, fs.ErrNotExist) {
		return fmt.Errorf("custom locale not found")
	} else if err != nil {
		return err
	}
	return nil
}

func (m *Manager) findCustom(code string) string {
	code = normalizeCode(code)
	exact := filepath.Join(m.customDir, code+".json")
	if _, err := os.Stat(exact); err == nil {
		return exact
	}
	entries, err := os.ReadDir(m.customDir)
	if err != nil {
		return ""
	}
	for _, entry := range entries {
		if entry.IsDir() || !strings.HasSuffix(strings.ToLower(entry.Name()), ".json") {
			continue
		}
		base := strings.TrimSuffix(entry.Name(), filepath.Ext(entry.Name()))
		if normalizeCode(base) == code {
			return filepath.Join(m.customDir, entry.Name())
		}
	}
	return ""
}

func (m *Manager) IsBuiltin(code string) bool {
	for _, c := range builtinOrder {
		if c == normalizeCode(code) {
			return true
		}
	}
	return false
}

func (m *Manager) Template() (map[string]string, error) {
	en, err := m.readMessagesFS(m.embedded, "en.json")
	if err != nil {
		return nil, err
	}
	return en, nil
}

func normalizeCode(code string) string {
	code = strings.TrimSpace(code)
	if u, err := url.PathUnescape(code); err == nil {
		code = u
	}
	return strings.ReplaceAll(code, "_", "-")
}

func atomicWrite(path string, data []byte) error {
	dir := filepath.Dir(path)
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	tmp, err := os.CreateTemp(dir, ".locale-*")
	if err != nil {
		return err
	}
	tmpName := tmp.Name()
	defer os.Remove(tmpName)
	if _, err := tmp.Write(data); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Chmod(0o644); err != nil {
		tmp.Close()
		return err
	}
	if err := tmp.Close(); err != nil {
		return err
	}
	return os.Rename(tmpName, path)
}
