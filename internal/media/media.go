// Package media implements pluggable media libraries for the panel: a built-in
// local folder under the Hugo site, plus user-configured S3, WebDAV and local
// directories. Every backend exposes the same small CRUD surface so the HTTP
// layer stays uniform.
package media

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net"
	"net/url"
	"path"
	"path/filepath"
	"sort"
	"strings"
	"sync"
	"time"
	"unicode"
)

const (
	SettingsKey       = "media_libraries"
	DefaultID         = "local"
	DefaultMaxUpload  = 512 // MB
	MaxUploadLimit    = 4096
	builtinName       = "Site images"
	builtinPublicBase = ""
)

// Config is a stored media library. Secrets live only in this struct and in
// SQLite; the HTTP layer always exposes View instead.
type Config struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"` // local, s3, webdav
	Root        string `json:"root,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Region      string `json:"region,omitempty"`
	Bucket      string `json:"bucket,omitempty"`
	AccessKey   string `json:"access_key,omitempty"`
	SecretKey   string `json:"secret_key,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	Username    string `json:"username,omitempty"`
	Password    string `json:"password,omitempty"`
	Prefix      string `json:"prefix,omitempty"`
	PublicBase  string `json:"public_base,omitempty"`
	PathStyle   bool   `json:"path_style,omitempty"`
	Insecure    bool   `json:"insecure_skip_verify,omitempty"`
	HostIP      string `json:"host_ip,omitempty"`
	DOHURL      string `json:"doh_url,omitempty"`
	MaxUploadMB int    `json:"max_upload_mb,omitempty"`
	Builtin     bool   `json:"builtin,omitempty"`
}

// View is the safe representation returned to the browser. Secret values are
// replaced by HasSecret so credentials are never sent back.
type View struct {
	ID          string `json:"id"`
	Name        string `json:"name"`
	Type        string `json:"type"`
	Root        string `json:"root,omitempty"`
	Endpoint    string `json:"endpoint,omitempty"`
	Region      string `json:"region,omitempty"`
	Bucket      string `json:"bucket,omitempty"`
	AccessKey   string `json:"access_key,omitempty"`
	BaseURL     string `json:"base_url,omitempty"`
	Username    string `json:"username,omitempty"`
	Prefix      string `json:"prefix,omitempty"`
	PublicBase  string `json:"public_base,omitempty"`
	PathStyle   bool   `json:"path_style,omitempty"`
	Insecure    bool   `json:"insecure_skip_verify,omitempty"`
	HostIP      string `json:"host_ip,omitempty"`
	DOHURL      string `json:"doh_url,omitempty"`
	MaxUploadMB int    `json:"max_upload_mb"`
	Builtin     bool   `json:"builtin"`
	HasSecret   bool   `json:"has_secret"`
}

func (c Config) View() View {
	return View{
		ID:          c.ID,
		Name:        c.Name,
		Type:        c.Type,
		Root:        c.Root,
		Endpoint:    c.Endpoint,
		Region:      c.Region,
		Bucket:      c.Bucket,
		AccessKey:   c.AccessKey,
		BaseURL:     c.BaseURL,
		Username:    c.Username,
		Prefix:      c.Prefix,
		PublicBase:  c.PublicBase,
		PathStyle:   c.PathStyle,
		Insecure:    c.Insecure,
		HostIP:      c.HostIP,
		DOHURL:      c.DOHURL,
		MaxUploadMB: c.MaxUploadMB,
		Builtin:     c.Builtin,
		HasSecret:   c.SecretKey != "" || c.Password != "",
	}
}

// Entry is a file or folder inside a library.
type Entry struct {
	Path        string `json:"path"`
	Name        string `json:"name"`
	IsDir       bool   `json:"is_dir"`
	Size        int64  `json:"size"`
	ModTime     string `json:"mod_time,omitempty"`
	ContentType string `json:"content_type,omitempty"`
	Kind        string `json:"kind,omitempty"`
	URL         string `json:"url,omitempty"`
	PreviewURL  string `json:"preview_url,omitempty"`
}

// Backend is the common storage surface. Keys are slash-separated relative
// paths with no leading slash. Read length < 0 means "to the end".
type Backend interface {
	List(ctx context.Context, dir string) ([]Entry, error)
	Stat(ctx context.Context, key string) (Entry, error)
	Read(ctx context.Context, key string, offset, length int64) (io.ReadCloser, error)
	Write(ctx context.Context, key string, r io.ReadSeeker, size int64, contentType string) error
	Delete(ctx context.Context, key string) error
	Move(ctx context.Context, from, to string) error
	Copy(ctx context.Context, from, to string) error
	Mkdir(ctx context.Context, dir string) error
	Test(ctx context.Context) error
	URL(key string) string
}

// Error is a storage error that can be mapped to an HTTP response.
type Error struct {
	Code    string
	Message string
}

func (e *Error) Error() string { return e.Message }

// IsCode reports whether err is a storage error with the given code.
func IsCode(err error, code string) bool {
	var target *Error
	return errors.As(err, &target) && target.Code == code
}

func errf(code, format string, args ...any) error {
	return &Error{Code: code, Message: fmt.Sprintf(format, args...)}
}

// Store is the slice of the SQLite store the manager needs.
type Store interface {
	GetSetting(ctx context.Context, key string) (string, bool, error)
	SetSetting(ctx context.Context, key, value string) error
}

// Manager loads library definitions and builds backends.
type Manager struct {
	mu      sync.Mutex
	store   Store
	hugoDir func(context.Context) string
}

func NewManager(store Store, hugoDir func(context.Context) string) *Manager {
	return &Manager{store: store, hugoDir: hugoDir}
}

func (m *Manager) builtin(ctx context.Context) Config {
	root := ""
	if m.hugoDir != nil {
		if dir := strings.TrimSpace(m.hugoDir(ctx)); dir != "" {
			root = filepath.Join(dir, "static", "images")
		}
	}
	return Config{
		ID:          DefaultID,
		Name:        builtinName,
		Type:        "local",
		Root:        root,
		PublicBase:  builtinPublicBase,
		MaxUploadMB: DefaultMaxUpload,
		Builtin:     true,
	}
}

func (m *Manager) stored(ctx context.Context) ([]Config, error) {
	raw, ok, err := m.store.GetSetting(ctx, SettingsKey)
	if err != nil {
		return nil, err
	}
	if !ok || strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	var list []Config
	if err := json.Unmarshal([]byte(raw), &list); err != nil {
		return nil, errf("server_error", "Stored media libraries could not be read")
	}
	out := list[:0]
	for _, cfg := range list {
		if cfg.ID == DefaultID || !validID(cfg.ID) {
			continue
		}
		out = append(out, cfg)
	}
	return out, nil
}

func (m *Manager) saveAll(ctx context.Context, list []Config) error {
	sort.SliceStable(list, func(i, j int) bool {
		return strings.ToLower(list[i].Name) < strings.ToLower(list[j].Name)
	})
	data, err := json.Marshal(list)
	if err != nil {
		return err
	}
	return m.store.SetSetting(ctx, SettingsKey, string(data))
}

// List returns the built-in local library first, then configured libraries.
func (m *Manager) List(ctx context.Context) ([]Config, error) {
	stored, err := m.stored(ctx)
	if err != nil {
		return nil, err
	}
	out := make([]Config, 0, len(stored)+1)
	out = append(out, m.builtin(ctx))
	out = append(out, stored...)
	return out, nil
}

func (m *Manager) Get(ctx context.Context, id string) (Config, error) {
	id = strings.TrimSpace(id)
	if id == "" || id == DefaultID {
		return m.builtin(ctx), nil
	}
	list, err := m.stored(ctx)
	if err != nil {
		return Config{}, err
	}
	for _, cfg := range list {
		if cfg.ID == id {
			return cfg, nil
		}
	}
	return Config{}, errf("not_found", "Storage library not found")
}

// Save creates or updates a library. Empty secrets on an update keep the
// previously stored values, matching the settings page behaviour.
func (m *Manager) Save(ctx context.Context, cfg Config) (Config, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	cfg.Builtin = false
	cfg.ID = strings.TrimSpace(cfg.ID)
	if cfg.ID == DefaultID {
		return Config{}, errf("invalid_request", "The built-in library cannot be edited")
	}
	autoID := cfg.ID == ""
	list, err := m.stored(ctx)
	if err != nil {
		return Config{}, err
	}
	found := -1
	for i, existing := range list {
		if existing.ID == cfg.ID {
			found = i
			if cfg.SecretKey == "" {
				cfg.SecretKey = existing.SecretKey
			}
			if cfg.Password == "" {
				cfg.Password = existing.Password
			}
			break
		}
	}
	if err := normalizeConfig(&cfg); err != nil {
		return Config{}, err
	}
	if found < 0 && autoID {
		cfg.ID = uniqueID(slug(cfg.Name), list)
	}
	if found >= 0 {
		list[found] = cfg
	} else {
		list = append(list, cfg)
	}
	if err := m.saveAll(ctx, list); err != nil {
		return Config{}, err
	}
	return cfg, nil
}

func (m *Manager) Delete(ctx context.Context, id string) error {
	m.mu.Lock()
	defer m.mu.Unlock()
	id = strings.TrimSpace(id)
	if id == "" || id == DefaultID {
		return errf("invalid_request", "The built-in library cannot be deleted")
	}
	list, err := m.stored(ctx)
	if err != nil {
		return err
	}
	out := list[:0]
	removed := false
	for _, cfg := range list {
		if cfg.ID == id {
			removed = true
			continue
		}
		out = append(out, cfg)
	}
	if !removed {
		return errf("not_found", "Storage library not found")
	}
	return m.saveAll(ctx, out)
}

// Backend resolves a library id into a usable backend.
func (m *Manager) Backend(ctx context.Context, id string) (Backend, Config, error) {
	cfg, err := m.Get(ctx, id)
	if err != nil {
		return nil, Config{}, err
	}
	if cfg.Type == "local" && cfg.Root == "" {
		return nil, Config{}, errf("not_initialized", "The Hugo site directory is not configured")
	}
	b, err := buildBackend(cfg)
	if err != nil {
		return nil, Config{}, err
	}
	return b, cfg, nil
}

// Test builds a backend from cfg (or merges stored secrets when editing) and
// verifies that it can be reached.
func (m *Manager) Test(ctx context.Context, cfg Config) error {
	cfg.ID = strings.TrimSpace(cfg.ID)
	if cfg.ID != "" && cfg.ID != DefaultID {
		if existing, err := m.Get(ctx, cfg.ID); err == nil {
			if cfg.SecretKey == "" {
				cfg.SecretKey = existing.SecretKey
			}
			if cfg.Password == "" {
				cfg.Password = existing.Password
			}
		}
	}
	if cfg.ID == "" || cfg.ID == DefaultID {
		if cfg.Type == "" {
			cfg.Type = "local"
		}
		if cfg.Type == "local" && cfg.Root == "" {
			cfg = m.builtin(ctx)
		}
	}
	if err := normalizeConfig(&cfg); err != nil {
		return err
	}
	b, err := buildBackend(cfg)
	if err != nil {
		return err
	}
	ctx, cancel := context.WithTimeout(ctx, 20*time.Second)
	defer cancel()
	return b.Test(ctx)
}

func buildBackend(cfg Config) (Backend, error) {
	switch cfg.Type {
	case "local":
		return newLocal(cfg)
	case "s3":
		return newS3(cfg)
	case "webdav":
		return newWebDAV(cfg)
	default:
		return nil, errf("invalid_request", "Unknown storage type %q", cfg.Type)
	}
}

func normalizeConfig(cfg *Config) error {
	cfg.Name = strings.TrimSpace(cfg.Name)
	if cfg.Name == "" || len([]rune(cfg.Name)) > 80 {
		return errf("invalid_request", "Storage name is required (maximum 80 characters)")
	}
	if len([]rune(cfg.Name)) == 0 {
		return errf("invalid_request", "Storage name is required")
	}
	if cfg.MaxUploadMB <= 0 {
		cfg.MaxUploadMB = DefaultMaxUpload
	}
	if cfg.MaxUploadMB > MaxUploadLimit {
		return errf("invalid_request", "Maximum upload size must be %d MB or less", MaxUploadLimit)
	}
	cfg.Prefix = strings.Trim(strings.ReplaceAll(strings.TrimSpace(cfg.Prefix), "\\", "/"), "/")
	if cfg.Prefix != "" {
		clean, err := CleanPath(cfg.Prefix)
		if err != nil {
			return err
		}
		cfg.Prefix = clean
	}
	if cfg.ID != "" && !validID(cfg.ID) {
		return errf("invalid_request", "Invalid storage id")
	}
	cfg.PublicBase = strings.TrimRight(strings.TrimSpace(cfg.PublicBase), "/")
	if cfg.PublicBase != "" {
		u, err := url.Parse(cfg.PublicBase)
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errf("invalid_request", "Public base URL must be an absolute http(s) URL")
		}
	}
	cfg.HostIP = strings.Trim(strings.TrimSpace(cfg.HostIP), "[]")
	if cfg.HostIP != "" && net.ParseIP(cfg.HostIP) == nil {
		return errf("invalid_request", "Host IP must be an IPv4 or IPv6 address")
	}
	cfg.DOHURL = strings.TrimSpace(cfg.DOHURL)
	if cfg.DOHURL != "" {
		u, err := url.Parse(cfg.DOHURL)
		if err != nil || u.Scheme != "https" || u.Host == "" {
			return errf("invalid_request", "DNS over HTTPS URL must be an absolute https URL")
		}
	}
	switch cfg.Type {
	case "local":
		root := strings.TrimSpace(cfg.Root)
		if root == "" {
			return errf("invalid_request", "Local directory is required")
		}
		if !filepath.IsAbs(root) {
			return errf("invalid_request", "Local directory must be an absolute path")
		}
		if strings.ContainsAny(root, "\x00\r\n") {
			return errf("invalid_request", "Invalid local directory")
		}
		cfg.Root = filepath.Clean(root)
		cfg.Endpoint, cfg.Bucket, cfg.AccessKey, cfg.SecretKey = "", "", "", ""
		cfg.BaseURL, cfg.Username, cfg.Password, cfg.PathStyle, cfg.Insecure = "", "", "", false, false
		cfg.HostIP, cfg.DOHURL = "", ""
	case "s3":
		u, err := url.Parse(strings.TrimSpace(cfg.Endpoint))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errf("invalid_request", "S3 endpoint must be an absolute http(s) URL")
		}
		cfg.Endpoint = strings.TrimRight(strings.TrimSpace(cfg.Endpoint), "/")
		cfg.Bucket = strings.TrimSpace(cfg.Bucket)
		if cfg.Bucket == "" || strings.ContainsAny(cfg.Bucket, "/\x00\r\n ") {
			return errf("invalid_request", "S3 bucket is required and must not contain spaces or slashes")
		}
		cfg.Region = strings.TrimSpace(cfg.Region)
		if cfg.Region == "" {
			cfg.Region = "us-east-1"
		}
		cfg.AccessKey = strings.TrimSpace(cfg.AccessKey)
		if cfg.AccessKey == "" || strings.ContainsAny(cfg.AccessKey, "\x00\r\n") {
			return errf("invalid_request", "S3 access key is required")
		}
		if cfg.SecretKey == "" || strings.ContainsAny(cfg.SecretKey, "\x00\r\n") {
			return errf("invalid_request", "S3 secret key is required")
		}
		cfg.Root, cfg.BaseURL, cfg.Username, cfg.Password = "", "", "", ""
	case "webdav":
		u, err := url.Parse(strings.TrimSpace(cfg.BaseURL))
		if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
			return errf("invalid_request", "WebDAV URL must be an absolute http(s) URL")
		}
		cfg.BaseURL = strings.TrimRight(strings.TrimSpace(cfg.BaseURL), "/")
		cfg.Username = strings.TrimSpace(cfg.Username)
		if strings.ContainsAny(cfg.Username, "\x00\r\n") || strings.ContainsAny(cfg.Password, "\x00\r\n") {
			return errf("invalid_request", "Invalid WebDAV credentials")
		}
		cfg.Root, cfg.Endpoint, cfg.Bucket, cfg.AccessKey, cfg.SecretKey, cfg.PathStyle = "", "", "", "", "", false
	default:
		return errf("invalid_request", "Choose a storage type")
	}
	return nil
}

func validID(id string) bool {
	if id == "" || len(id) > 64 {
		return false
	}
	for i, r := range id {
		ok := r >= 'a' && r <= 'z' || r >= '0' && r <= '9' || r == '-' || r == '_'
		if !ok {
			return false
		}
		if i == 0 && !(r >= 'a' && r <= 'z' || r >= '0' && r <= '9') {
			return false
		}
	}
	return true
}

func uniqueID(base string, existing []Config) string {
	if base == "" {
		base = "library"
	}
	seen := map[string]bool{}
	for _, cfg := range existing {
		seen[cfg.ID] = true
	}
	if !seen[base] {
		return base
	}
	for i := 2; i < 1000; i++ {
		candidate := fmt.Sprintf("%s-%d", base, i)
		if !seen[candidate] {
			return candidate
		}
	}
	return fmt.Sprintf("library-%d", time.Now().UnixNano())
}

func slug(s string) string {
	s = strings.ToLower(strings.TrimSpace(s))
	var b strings.Builder
	last := false
	for _, r := range s {
		if r >= 'a' && r <= 'z' || r >= '0' && r <= '9' {
			b.WriteRune(r)
			last = false
		} else if !last && b.Len() > 0 {
			b.WriteByte('-')
			last = true
		}
	}
	out := strings.Trim(b.String(), "-")
	if out == "" {
		out = "library"
	}
	if len(out) > 48 {
		out = out[:48]
	}
	return out
}

// CleanPath validates a relative, slash-separated storage key or folder.
func CleanPath(p string) (string, error) {
	p = strings.TrimSpace(strings.ReplaceAll(p, "\\", "/"))
	if p == "" {
		return "", nil
	}
	if strings.HasPrefix(p, "/") {
		return "", errf("invalid_path", "Path must be relative")
	}
	if strings.ContainsAny(p, "\x00\r\n") {
		return "", errf("invalid_path", "Path contains invalid characters")
	}
	p = strings.TrimRight(p, "/")
	for _, part := range strings.Split(p, "/") {
		if part == "" || part == "." || part == ".." {
			return "", errf("invalid_path", "Path is not allowed")
		}
		if len(part) > 255 {
			return "", errf("invalid_path", "Path component is too long")
		}
	}
	if len(p) > 1024 {
		return "", errf("invalid_path", "Path is too long")
	}
	return p, nil
}

// SanitizeFilename turns an uploaded name into a safe ASCII-ish file name while
// keeping the extension. Unicode letters are preserved.
func SanitizeFilename(name string) string {
	name = strings.ReplaceAll(name, "\\", "/")
	name = path.Base(name)
	ext := strings.ToLower(path.Ext(name))
	base := strings.TrimSuffix(name, path.Ext(name))
	var b strings.Builder
	lastDash := false
	for _, r := range base {
		if unicode.IsLetter(r) || unicode.IsNumber(r) || r == '.' || r == '_' {
			b.WriteRune(r)
			lastDash = false
		} else if r == '-' || r == ' ' {
			if !lastDash && b.Len() > 0 {
				b.WriteByte('-')
				lastDash = true
			}
		}
	}
	out := strings.Trim(b.String(), ".-_")
	if out == "" {
		out = "file"
	}
	runes := []rune(out)
	if len(runes) > 100 {
		out = string(runes[:100])
	}
	return out + ext
}

func joinKey(dir, name string) string {
	dir = strings.Trim(dir, "/")
	if dir == "" {
		return name
	}
	return dir + "/" + name
}

func joinURL(base, key string) string {
	key = strings.TrimLeft(key, "/")
	if base == "" {
		return key
	}
	return strings.TrimRight(base, "/") + "/" + escapePath(key)
}

// escapePath percent-encodes each segment of a slash-separated path.
func escapePath(p string) string {
	parts := strings.Split(p, "/")
	for i, part := range parts {
		parts[i] = escapeSegment(part)
	}
	return strings.Join(parts, "/")
}

func escapeSegment(s string) string {
	const hex = "0123456789ABCDEF"
	var b strings.Builder
	for i := 0; i < len(s); i++ {
		c := s[i]
		if c >= 'a' && c <= 'z' || c >= 'A' && c <= 'Z' || c >= '0' && c <= '9' || c == '-' || c == '_' || c == '.' || c == '~' {
			b.WriteByte(c)
		} else {
			b.WriteByte('%')
			b.WriteByte(hex[c>>4])
			b.WriteByte(hex[c&15])
		}
	}
	return b.String()
}

// previewURL points at the panel proxy so private libraries can still preview.
func previewURL(id, key string) string {
	q := url.Values{}
	q.Set("library", id)
	q.Set("path", key)
	return "/api/media/file?" + q.Encode()
}

// ApplyURLs fills the URL and preview URL fields of entries.
func ApplyURLs(b Backend, id string, entries []Entry) {
	for i := range entries {
		entries[i].URL = b.URL(entries[i].Path)
		entries[i].PreviewURL = previewURL(id, entries[i].Path)
	}
}

func kindOf(contentType string) string {
	ct := strings.ToLower(strings.Split(contentType, ";")[0])
	switch {
	case strings.HasPrefix(ct, "image/"):
		return "image"
	case strings.HasPrefix(ct, "video/"):
		return "video"
	case strings.HasPrefix(ct, "audio/"):
		return "audio"
	case ct == "application/pdf":
		return "pdf"
	case strings.HasPrefix(ct, "text/"):
		return "text"
	default:
		return "file"
	}
}

func sortedEntries(entries []Entry) {
	sort.Slice(entries, func(i, j int) bool {
		if entries[i].IsDir != entries[j].IsDir {
			return entries[i].IsDir
		}
		return strings.ToLower(entries[i].Name) < strings.ToLower(entries[j].Name)
	})
}

func detectContentType(name string) string {
	ext := strings.ToLower(path.Ext(name))
	if ct, ok := contentTypes[ext]; ok {
		return ct
	}
	// Anything outside the allowlist is downloaded, never rendered inline.
	return "application/octet-stream"
}

// SupportedUpload reports whether a file type may be uploaded.
func SupportedUpload(name string) bool {
	_, ok := contentTypes[strings.ToLower(path.Ext(name))]
	return ok
}

// AllowedExtensions returns the accept attribute for file inputs.
func AllowedExtensions() []string {
	out := make([]string, 0, len(contentTypes))
	for ext := range contentTypes {
		out = append(out, ext)
	}
	sort.Strings(out)
	return out
}
