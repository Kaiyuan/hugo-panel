package server

import (
	"compress/gzip"
	"context"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"mime"
	"net"
	"net/http"
	"net/url"
	"path"
	"strings"
	"sync"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/config"
	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/i18n"
	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/media"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
	"github.com/Kaiyuan/hugo-panel/web"
)

const sessionCookie = "hugo_panel_session"

type Server struct {
	cfg          *config.Config
	store        *store.Store
	settings     *settings.Manager
	i18n         *i18n.Manager
	mailer       *mailer.Mailer
	hugo         *hugo.Service
	media        *media.Manager
	logger       *slog.Logger
	version      string
	assetVersion string
	started      time.Time

	staticFS fs.FS
	index    []byte

	limiter   *rateLimiter
	dummyHash string
}

type apiFunc func(http.ResponseWriter, *http.Request, store.Session)

type ctxKey int

const sessionKey ctxKey = 1

func New(cfg *config.Config, st *store.Store, set *settings.Manager, tr *i18n.Manager, ml *mailer.Mailer, hs *hugo.Service, logger *slog.Logger, version string) (*Server, error) {
	version = normalizeVersion(version)
	staticFS, err := fs.Sub(web.Files, "static")
	if err != nil {
		return nil, err
	}
	index, err := fs.ReadFile(staticFS, "index.html")
	if err != nil {
		return nil, fmt.Errorf("read index.html: %w", err)
	}
	return &Server{
		cfg:          cfg,
		store:        st,
		settings:     set,
		i18n:         tr,
		mailer:       ml,
		hugo:         hs,
		media:        media.NewManager(st, func(ctx context.Context) string { return set.HugoDir(ctx) }),
		logger:       logger,
		version:      version,
		assetVersion: computeAssetVersion(version, index, staticFS),
		started:      time.Now(),
		staticFS:     staticFS,
		index:        index,
		limiter:      newRateLimiter(),
		dummyHash:    "$2a$12$R9h/cIPz0gi.URNNX3kh2OPST9/PgBkqquzi.Ss7KIUgO2t0jWMUW",
	}, nil
}

func normalizeVersion(version string) string {
	version = strings.TrimSpace(version)
	version = strings.TrimPrefix(version, "v")
	if version == "" {
		return "dev"
	}
	return version
}

func computeAssetVersion(version string, index []byte, staticFS fs.FS) string {
	h := sha256.New()
	h.Write([]byte("index.html\x00"))
	h.Write(index)
	h.Write([]byte{0})
	_ = fs.WalkDir(staticFS, ".", func(name string, entry fs.DirEntry, err error) error {
		if err != nil || entry.IsDir() {
			return nil
		}
		data, readErr := fs.ReadFile(staticFS, name)
		if readErr != nil {
			return nil
		}
		h.Write([]byte(name))
		h.Write([]byte{0})
		h.Write(data)
		h.Write([]byte{0})
		return nil
	})
	return version + "." + hex.EncodeToString(h.Sum(nil))[:12]
}

func (s *Server) Handler() http.Handler {
	mux := http.NewServeMux()

	mux.HandleFunc("GET /api/health", s.handleHealth)
	mux.HandleFunc("GET /api/state", s.handleState)
	mux.HandleFunc("POST /api/setup", s.handleSetup)
	mux.HandleFunc("POST /api/login", s.handleLogin)
	mux.HandleFunc("POST /api/logout", s.requireAuth(s.handleLogout))
	mux.HandleFunc("POST /api/password/forgot", s.handleForgotPassword)
	mux.HandleFunc("POST /api/password/reset", s.handleResetPassword)

	mux.HandleFunc("GET /api/i18n/languages", s.handleLanguages)
	mux.HandleFunc("GET /api/i18n/{code}", s.handleMessages)

	mux.HandleFunc("GET /api/me", s.requireAuth(s.handleMe))
	mux.HandleFunc("PUT /api/me", s.requireAuth(s.handleUpdateMe))
	mux.HandleFunc("POST /api/me/password", s.requireAuth(s.handleChangePassword))

	mux.HandleFunc("GET /api/dashboard", s.requireAuth(s.handleDashboard))

	mux.HandleFunc("GET /api/content", s.requireAuth(s.handleContentList))
	mux.HandleFunc("GET /api/content/meta", s.requireAuth(s.handleContentMeta))
	mux.HandleFunc("GET /api/content/item", s.requireAuth(s.handleContentGet))
	mux.HandleFunc("POST /api/content", s.requireAuth(s.handleContentCreate))
	mux.HandleFunc("PUT /api/content", s.requireAuth(s.handleContentUpdate))
	mux.HandleFunc("DELETE /api/content", s.requireAuth(s.handleContentDelete))

	mux.HandleFunc("GET /api/hugo/config", s.requireAuth(s.handleConfigGet))
	mux.HandleFunc("PUT /api/hugo/config", s.requireAuth(s.handleConfigSave))
	mux.HandleFunc("POST /api/hugo/init", s.requireAuth(s.handleHugoInit))
	mux.HandleFunc("GET /api/system", s.requireAuth(s.handleSystem))

	mux.HandleFunc("GET /api/themes", s.requireAuth(s.handleThemes))
	mux.HandleFunc("POST /api/themes/activate", s.requireAuth(s.handleThemeActivate))
	mux.HandleFunc("POST /api/themes/install", s.requireAuth(s.handleThemeInstall))
	mux.HandleFunc("POST /api/themes/update", s.requireAuth(s.handleThemeUpdate))
	mux.HandleFunc("DELETE /api/themes", s.requireAuth(s.handleThemeRemove))
	mux.HandleFunc("GET /api/themes/screenshot", s.requireAuth(s.handleThemeScreenshot))

	mux.HandleFunc("GET /api/plugins", s.requireAuth(s.handlePlugins))
	mux.HandleFunc("POST /api/plugins", s.requireAuth(s.handlePluginAdd))
	mux.HandleFunc("DELETE /api/plugins", s.requireAuth(s.handlePluginRemove))
	mux.HandleFunc("POST /api/plugins/tidy", s.requireAuth(s.handlePluginTidy))

	mux.HandleFunc("GET /api/build", s.requireAuth(s.handleBuildGet))
	mux.HandleFunc("POST /api/build", s.requireAuth(s.handleBuildRun))

	mux.HandleFunc("GET /api/settings", s.requireAuth(s.handleSettingsGet))
	mux.HandleFunc("PUT /api/settings", s.requireAuth(s.handleSettingsSave))
	mux.HandleFunc("POST /api/settings/test-email", s.requireAuth(s.handleTestEmail))

	mux.HandleFunc("GET /api/locales", s.requireAuth(s.handleLocales))
	mux.HandleFunc("POST /api/locales", s.requireAuth(s.handleLocaleAdd))
	mux.HandleFunc("DELETE /api/locales", s.requireAuth(s.handleLocaleDelete))
	mux.HandleFunc("GET /api/locales/template", s.requireAuth(s.handleLocaleTemplate))

	mux.HandleFunc("GET /api/media", s.requireAuth(s.handleMediaList))
	mux.HandleFunc("POST /api/media", s.requireAuth(s.handleMediaUpload))
	mux.HandleFunc("DELETE /api/media", s.requireAuth(s.handleMediaDelete))
	mux.HandleFunc("GET /api/media/file", s.requireAuth(s.handleMediaFile))
	mux.HandleFunc("GET /api/media/libraries", s.requireAuth(s.handleMediaLibraries))
	mux.HandleFunc("POST /api/media/libraries", s.requireAuth(s.handleMediaLibrarySave))
	mux.HandleFunc("DELETE /api/media/libraries", s.requireAuth(s.handleMediaLibraryDelete))
	mux.HandleFunc("POST /api/media/libraries/test", s.requireAuth(s.handleMediaLibraryTest))
	mux.HandleFunc("POST /api/media/move", s.requireAuth(s.handleMediaMove))
	mux.HandleFunc("POST /api/media/copy", s.requireAuth(s.handleMediaCopy))
	mux.HandleFunc("POST /api/media/folder", s.requireAuth(s.handleMediaFolder))

	mux.HandleFunc("GET /favicon.ico", s.handleFavicon)
	mux.HandleFunc("GET /manifest.webmanifest", s.handleManifest)
	mux.HandleFunc("GET /sw.js", s.handleServiceWorker)
	mux.HandleFunc("GET /static/", s.handleStatic)
	mux.HandleFunc("/", s.handleSPA)

	return s.recoverMiddleware(s.logMiddleware(s.securityMiddleware(s.originMiddleware(mux))))
}

func (s *Server) handleHealth(w http.ResponseWriter, r *http.Request) {
	ctx, cancel := context.WithTimeout(r.Context(), 2*time.Second)
	defer cancel()
	if _, err := s.store.CountUsers(ctx); err != nil {
		writeError(w, http.StatusServiceUnavailable, "database_error", "Database is unavailable")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"status": "ok", "version": s.version})
}

type stateDefaults struct {
	Language string `json:"language"`
	Theme    string `json:"theme"`
}

type stateResponse struct {
	SetupRequired  bool            `json:"setup_required"`
	Authenticated  bool            `json:"authenticated"`
	User           *store.User     `json:"user,omitempty"`
	CSRF           string          `json:"csrf,omitempty"`
	Version        string          `json:"version"`
	Languages      []i18n.Language `json:"languages"`
	Defaults       stateDefaults   `json:"defaults"`
	SMTPConfigured bool            `json:"smtp_configured"`
	StartedAt      string          `json:"started_at"`
}

func (s *Server) buildState(r *http.Request) (stateResponse, error) {
	return s.buildStateWithSession(r, nil)
}

func (s *Server) buildStateWithSession(r *http.Request, override *store.Session) (stateResponse, error) {
	ctx := r.Context()
	count, err := s.store.CountUsers(ctx)
	if err != nil {
		return stateResponse{}, err
	}
	languages, err := s.i18n.Languages()
	if err != nil {
		languages = []i18n.Language{{Code: "en", Name: "English", NativeName: "English", Builtin: true}}
	}
	lang, _ := s.settings.Get(ctx, settings.KeyDefaultLanguage)
	theme, _ := s.settings.Get(ctx, settings.KeyDefaultTheme)
	if lang == "" {
		lang = "en"
	}
	foundLang := false
	for _, l := range languages {
		if l.Code == lang {
			foundLang = true
			break
		}
	}
	if !foundLang {
		lang = "en"
	}
	if theme != "light" && theme != "dark" && theme != "system" {
		theme = "system"
	}
	smtpCfg, _ := s.settings.EffectiveSMTP(ctx)
	state := stateResponse{
		SetupRequired:  count == 0,
		Version:        s.version,
		Languages:      languages,
		Defaults:       stateDefaults{Language: lang, Theme: theme},
		SMTPConfigured: smtpCfg.Configured(),
		StartedAt:      s.started.UTC().Format(time.RFC3339),
	}
	if override != nil {
		u := override.User
		state.Authenticated = true
		state.User = &u
		state.CSRF = override.CSRF
		return state, nil
	}
	if token, err := s.sessionToken(r); err == nil && token != "" {
		if sess, err := s.store.GetSession(ctx, token); err == nil {
			u := sess.User
			state.Authenticated = true
			state.User = &u
			state.CSRF = sess.CSRF
		}
	}
	return state, nil
}

func (s *Server) handleState(w http.ResponseWriter, r *http.Request) {
	state, err := s.buildState(r)
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load application state")
		return
	}
	writeJSON(w, http.StatusOK, state)
}

func (s *Server) requireAuth(next apiFunc) http.HandlerFunc {
	return func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Cache-Control", "no-store")
		token, err := s.sessionToken(r)
		if err != nil {
			writeError(w, http.StatusUnauthorized, "session_expired", "Your session has expired")
			return
		}
		sess, err := s.store.GetSession(r.Context(), token)
		if err != nil {
			clearSessionCookie(w, r, s.cfg.CookieSecure)
			writeError(w, http.StatusUnauthorized, "session_expired", "Your session has expired")
			return
		}
		if r.Method != http.MethodGet && r.Method != http.MethodHead && r.Method != http.MethodOptions {
			csrf := r.Header.Get("X-CSRF-Token")
			if csrf == "" || subtle.ConstantTimeCompare([]byte(csrf), []byte(sess.CSRF)) != 1 {
				writeError(w, http.StatusForbidden, "csrf_invalid", "Invalid security token. Refresh the page and try again.")
				return
			}
		}
		ctx := context.WithValue(r.Context(), sessionKey, sess)
		next(w, r.WithContext(ctx), sess)
	}
}

func sessionFrom(r *http.Request) store.Session {
	sess, _ := r.Context().Value(sessionKey).(store.Session)
	return sess
}

func (s *Server) sessionToken(r *http.Request) (string, error) {
	c, err := r.Cookie(sessionCookie)
	if err != nil {
		return "", err
	}
	return c.Value, nil
}

func (s *Server) recoverMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		defer func() {
			if rec := recover(); rec != nil {
				s.logger.Error("panic", "error", rec, "path", r.URL.Path)
				writeError(w, http.StatusInternalServerError, "server_error", "Internal server error")
			}
		}()
		next.ServeHTTP(w, r)
	})
}

type statusRecorder struct {
	http.ResponseWriter
	status int
	bytes  int
}

func (w *statusRecorder) WriteHeader(code int) {
	if w.status == 0 {
		w.status = code
	}
	w.ResponseWriter.WriteHeader(code)
}

func (w *statusRecorder) Write(b []byte) (int, error) {
	if w.status == 0 {
		w.status = http.StatusOK
	}
	n, err := w.ResponseWriter.Write(b)
	w.bytes += n
	return n, err
}

func (w *statusRecorder) Flush() {
	if f, ok := w.ResponseWriter.(http.Flusher); ok {
		f.Flush()
	}
}

func (s *Server) logMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		start := time.Now()
		rec := &statusRecorder{ResponseWriter: w}
		next.ServeHTTP(rec, r)
		if rec.status == 0 {
			rec.status = http.StatusOK
		}
		level := slog.LevelInfo
		if rec.status >= 500 {
			level = slog.LevelError
		} else if rec.status >= 400 {
			level = slog.LevelWarn
		} else if strings.HasPrefix(r.URL.Path, "/static/") || r.URL.Path == "/api/health" || r.URL.Path == "/sw.js" || r.URL.Path == "/manifest.webmanifest" {
			level = slog.LevelDebug
		}
		s.logger.Log(r.Context(), level, "request",
			"method", r.Method,
			"path", r.URL.Path,
			"status", rec.status,
			"bytes", rec.bytes,
			"duration", time.Since(start).Round(time.Millisecond).String(),
			"ip", clientIP(r),
		)
	})
}

func (s *Server) securityMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		h := w.Header()
		h.Set("X-Content-Type-Options", "nosniff")
		h.Set("X-Frame-Options", "DENY")
		h.Set("X-Robots-Tag", "noindex, nofollow")
		h.Set("Referrer-Policy", "strict-origin-when-cross-origin")
		h.Set("Permissions-Policy", "camera=(), microphone=(), geolocation=()")
		h.Set("Content-Security-Policy", "default-src 'self'; script-src 'self' 'unsafe-eval'; style-src 'self' 'unsafe-inline'; img-src 'self' data: blob: https: http:; font-src 'self' data:; connect-src 'self'; media-src 'self' blob: https: http:; manifest-src 'self'; object-src 'none'; base-uri 'none'; frame-ancestors 'none'; form-action 'self'")
		if isSecureRequest(r) {
			h.Set("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		}
		if strings.HasPrefix(r.URL.Path, "/api/") {
			h.Set("Cache-Control", "no-store")
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originMiddleware(next http.Handler) http.Handler {
	return http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		switch r.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			next.ServeHTTP(w, r)
			return
		}
		if origin := r.Header.Get("Origin"); origin != "" {
			if !s.originAllowed(r, origin) {
				writeError(w, http.StatusForbidden, "forbidden", "Cross-origin request rejected")
				return
			}
		}
		if strings.EqualFold(r.Header.Get("Sec-Fetch-Site"), "cross-site") {
			writeError(w, http.StatusForbidden, "forbidden", "Cross-site request rejected")
			return
		}
		next.ServeHTTP(w, r)
	})
}

func (s *Server) originAllowed(r *http.Request, origin string) bool {
	u, err := url.Parse(origin)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") || u.Host == "" {
		return false
	}
	candidates := []string{r.Host}
	if xfh := r.Header.Get("X-Forwarded-Host"); xfh != "" {
		candidates = append(candidates, strings.Split(xfh, ",")...)
	}
	if pub := s.settings.PublicURL(r.Context()); pub != "" {
		if pu, err := url.Parse(pub); err == nil {
			candidates = append(candidates, pu.Host)
		}
	}
	for _, c := range candidates {
		if strings.EqualFold(strings.TrimSpace(c), u.Host) {
			return true
		}
	}
	return false
}

func (s *Server) handleSPA(w http.ResponseWriter, r *http.Request) {
	if r.URL.Path == "/api" || strings.HasPrefix(r.URL.Path, "/api/") {
		writeError(w, http.StatusNotFound, "not_found", "Endpoint not found")
		return
	}
	if r.Method != http.MethodGet && r.Method != http.MethodHead {
		w.Header().Set("Allow", "GET, HEAD")
		writeError(w, http.StatusMethodNotAllowed, "method_not_allowed", "Method not allowed")
		return
	}
	if r.URL.Path != "/" && r.URL.Path != "/index.html" {
		// Hash routing is used, so unknown paths still receive the app shell.
	}
	w.Header().Set("Content-Type", "text/html; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(s.index)
}

func (s *Server) handleFavicon(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.staticFS, "icons/favicon-32.png")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Favicon not found")
		return
	}
	w.Header().Set("Content-Type", "image/png")
	w.Header().Set("Cache-Control", "public, max-age=86400")
	_, _ = w.Write(data)
}

func (s *Server) handleManifest(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.staticFS, "manifest.webmanifest")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Manifest not found")
		return
	}
	w.Header().Set("Content-Type", "application/manifest+json")
	w.Header().Set("Cache-Control", "public, max-age=3600")
	_, _ = w.Write(data)
}

func (s *Server) handleServiceWorker(w http.ResponseWriter, r *http.Request) {
	data, err := fs.ReadFile(s.staticFS, "sw.js")
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Service worker not found")
		return
	}
	body := strings.ReplaceAll(string(data), "__PANEL_VERSION__", s.assetVersion)
	w.Header().Set("Content-Type", "text/javascript; charset=utf-8")
	w.Header().Set("Cache-Control", "no-cache")
	w.Header().Set("Service-Worker-Allowed", "/")
	_, _ = w.Write([]byte(body))
}

func (s *Server) handleStatic(w http.ResponseWriter, r *http.Request) {
	rel := strings.TrimPrefix(r.URL.Path, "/static/")
	if rel == "" || !fs.ValidPath(rel) || strings.HasSuffix(r.URL.Path, "/") {
		writeError(w, http.StatusNotFound, "not_found", "File not found")
		return
	}
	data, err := fs.ReadFile(s.staticFS, rel)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "File not found")
		return
	}
	ctype := mime.TypeByExtension(path.Ext(rel))
	switch path.Ext(rel) {
	case ".js", ".mjs":
		ctype = "text/javascript; charset=utf-8"
	case ".css":
		ctype = "text/css; charset=utf-8"
	case ".svg":
		ctype = "image/svg+xml"
	case ".woff2":
		ctype = "font/woff2"
	case ".webmanifest":
		ctype = "application/manifest+json"
	case ".json":
		ctype = "application/json; charset=utf-8"
	}
	if ctype == "" {
		ctype = "application/octet-stream"
	}
	w.Header().Set("Content-Type", ctype)
	switch {
	case strings.HasPrefix(rel, "vendor/"), strings.HasPrefix(rel, "fonts/"):
		w.Header().Set("Cache-Control", "public, max-age=86400")
	case strings.HasPrefix(rel, "icons/"):
		w.Header().Set("Cache-Control", "public, max-age=3600")
	default:
		w.Header().Set("Cache-Control", "no-cache")
	}
	if body, ok := maybeGzip(r, data, ctype); ok {
		w.Header().Set("Content-Encoding", "gzip")
		w.Header().Set("Vary", "Accept-Encoding")
		data = body
	}
	w.Header().Set("Content-Length", fmt.Sprintf("%d", len(data)))
	if r.Method == http.MethodHead {
		return
	}
	_, _ = w.Write(data)
}

func maybeGzip(r *http.Request, data []byte, ctype string) ([]byte, bool) {
	if len(data) < 1024 || !strings.Contains(r.Header.Get("Accept-Encoding"), "gzip") {
		return nil, false
	}
	if !(strings.HasPrefix(ctype, "text/") || strings.Contains(ctype, "javascript") || strings.Contains(ctype, "json") || strings.Contains(ctype, "svg") || strings.Contains(ctype, "manifest")) {
		return nil, false
	}
	var b strings.Builder
	gz := gzip.NewWriter(&b)
	if _, err := gz.Write(data); err != nil {
		return nil, false
	}
	if err := gz.Close(); err != nil {
		return nil, false
	}
	return []byte(b.String()), true
}

func writeJSON(w http.ResponseWriter, status int, v any) {
	w.Header().Set("Content-Type", "application/json; charset=utf-8")
	w.WriteHeader(status)
	enc := json.NewEncoder(w)
	enc.SetEscapeHTML(false)
	_ = enc.Encode(v)
}

func writeError(w http.ResponseWriter, status int, code, message string) {
	writeJSON(w, status, map[string]string{"error": code, "message": message})
}

func decodeJSON(w http.ResponseWriter, r *http.Request, dst any, limit int64) bool {
	r.Body = http.MaxBytesReader(w, r.Body, limit)
	dec := json.NewDecoder(r.Body)
	if err := dec.Decode(dst); err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Invalid JSON request body")
		return false
	}
	var extra any
	if err := dec.Decode(&extra); err == nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Request body must contain a single JSON object")
		return false
	}
	return true
}

func isSecureRequest(r *http.Request) bool {
	if r.TLS != nil {
		return true
	}
	proto := strings.TrimSpace(strings.Split(r.Header.Get("X-Forwarded-Proto"), ",")[0])
	return strings.EqualFold(proto, "https")
}

func setSessionCookie(w http.ResponseWriter, r *http.Request, secMode, token string, expires time.Time) {
	secure := secMode == "true" || (secMode == "auto" && isSecureRequest(r))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    token,
		Path:     "/",
		Expires:  expires,
		MaxAge:   int(time.Until(expires).Seconds()),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clearSessionCookie(w http.ResponseWriter, r *http.Request, secMode string) {
	secure := secMode == "true" || (secMode == "auto" && isSecureRequest(r))
	http.SetCookie(w, &http.Cookie{
		Name:     sessionCookie,
		Value:    "",
		Path:     "/",
		MaxAge:   -1,
		Expires:  time.Unix(0, 0),
		HttpOnly: true,
		Secure:   secure,
		SameSite: http.SameSiteLaxMode,
	})
}

func clientIP(r *http.Request) string {
	host, _, err := net.SplitHostPort(r.RemoteAddr)
	if err != nil {
		return r.RemoteAddr
	}
	return host
}

type rateBucket struct {
	count int
	reset time.Time
}

type rateLimiter struct {
	mu      sync.Mutex
	buckets map[string]*rateBucket
}

func newRateLimiter() *rateLimiter {
	return &rateLimiter{buckets: map[string]*rateBucket{}}
}

func (l *rateLimiter) Allow(key string, limit int, window time.Duration) bool {
	now := time.Now()
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.buckets) > 4096 {
		for k, b := range l.buckets {
			if now.After(b.reset) {
				delete(l.buckets, k)
			}
		}
	}
	b, ok := l.buckets[key]
	if !ok || now.After(b.reset) {
		l.buckets[key] = &rateBucket{count: 1, reset: now.Add(window)}
		return true
	}
	if b.count >= limit {
		return false
	}
	b.count++
	return true
}

func (s *Server) site(ctx context.Context) *hugo.Site {
	return s.hugo.Site(s.settings.HugoDir(ctx), s.settings.HugoBinary(ctx), s.cfg.DataDir)
}

func writeHugoError(w http.ResponseWriter, err error) {
	var me *media.Error
	if errors.As(err, &me) {
		status := http.StatusBadRequest
		switch me.Code {
		case "not_found":
			status = http.StatusNotFound
		case "forbidden":
			status = http.StatusForbidden
		case "file_exists", "conflict", "not_initialized":
			status = http.StatusConflict
		case "server_error":
			status = http.StatusInternalServerError
		}
		writeError(w, status, me.Code, me.Message)
		return
	}
	var he *hugo.Error
	if errors.As(err, &he) {
		status := http.StatusBadRequest
		switch he.Code {
		case "not_found", "theme_not_found":
			status = http.StatusNotFound
		case "file_exists", "theme_active", "plugin_exists", "not_initialized", "conflict":
			status = http.StatusConflict
		case "hugo_not_found":
			status = http.StatusServiceUnavailable
		case "git_failed", "smtp_failed":
			status = http.StatusBadGateway
		case "server_error":
			status = http.StatusInternalServerError
		}
		writeError(w, status, he.Code, he.Message)
		return
	}
	writeError(w, http.StatusInternalServerError, "server_error", err.Error())
}
