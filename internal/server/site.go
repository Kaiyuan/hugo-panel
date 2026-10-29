package server

import (
	"net/http"
	"os"
	"runtime"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

func (s *Server) handleConfigGet(w http.ResponseWriter, r *http.Request, sess store.Session) {
	cfg, err := s.site(r.Context()).LoadConfig()
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleConfigSave(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var patch hugo.ConfigPatch
	if !decodeJSON(w, r, &patch, 4*1024*1024) {
		return
	}
	cfg, err := s.site(r.Context()).SaveConfig(patch)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

type initRequest struct {
	Title    string `json:"title"`
	BaseURL  string `json:"base_url"`
	Language string `json:"language"`
}

func (s *Server) handleHugoInit(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req initRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	site := s.site(r.Context())
	if _, err := site.EnsureHugoVersion(r.Context()); err != nil {
		writeHugoError(w, err)
		return
	}
	if err := site.InitSite(req.Title, req.BaseURL, req.Language); err != nil {
		writeHugoError(w, err)
		return
	}
	cfg, err := site.LoadConfig()
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, cfg)
}

func (s *Server) handleSystem(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	site := s.site(ctx)
	state, _ := site.PluginState(ctx)
	cfg, _ := site.LoadConfig()
	writeJSON(w, http.StatusOK, map[string]any{
		"panel_version":    s.version,
		"go_version":       runtime.Version(),
		"os":               runtime.GOOS,
		"arch":             runtime.GOARCH,
		"uptime_seconds":   int64(time.Since(s.started).Seconds()),
		"data_dir":         s.cfg.DataDir,
		"hugo_dir":         site.Dir,
		"hugo_binary":      site.Binary,
		"hugo_available":   state.HugoAvailable,
		"hugo_version":     state.HugoVersion,
		"hugo_min_version": hugo.MinHugoVersion,
		"hugo_supported":   state.HugoAvailable && hugo.HugoVersionAtLeast(state.HugoVersion, hugo.MinHugoVersion),
		"go_available":     state.GoAvailable,
		"git_available":    state.GitAvailable,
		"config_exists":    cfg.Exists,
		"config_path":      cfg.ConfigPath,
		"site_title":       cfg.SiteTitle,
		"base_url":         cfg.BaseURL,
		"active_theme":     cfg.Theme,
		"content_dir":      site.ContentDir(),
		"media_dir":        site.MediaRoot(),
		"public_url":       s.settings.PublicURL(ctx),
	})
}

func (s *Server) handleThemes(w http.ResponseWriter, r *http.Request, sess store.Session) {
	site := s.site(r.Context())
	themes, err := site.ListThemes()
	if err != nil {
		writeHugoError(w, err)
		return
	}
	cfg, _ := site.LoadConfig()
	writeJSON(w, http.StatusOK, map[string]any{"themes": themes, "active": cfg.Theme})
}

type themeNameRequest struct {
	Name string `json:"name"`
}

func (s *Server) handleThemeActivate(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req themeNameRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	if err := s.site(r.Context()).ActivateTheme(req.Name); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true, "active": true})
}

type themeInstallRequest struct {
	URL  string `json:"url"`
	Name string `json:"name"`
}

func (s *Server) handleThemeInstall(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req themeInstallRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	theme, err := s.site(r.Context()).InstallTheme(r.Context(), req.URL, req.Name)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"theme": theme})
}

func (s *Server) handleThemeUpdate(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req themeNameRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	output, err := s.site(r.Context()).UpdateTheme(r.Context(), req.Name)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"ok": true, "output": output})
}

func (s *Server) handleThemeRemove(w http.ResponseWriter, r *http.Request, sess store.Session) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Theme name is required")
		return
	}
	if err := s.site(r.Context()).RemoveTheme(name); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleThemeScreenshot(w http.ResponseWriter, r *http.Request, sess store.Session) {
	name := strings.TrimSpace(r.URL.Query().Get("name"))
	if name == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Theme name is required")
		return
	}
	full, err := s.site(r.Context()).ThemeScreenshotFile(name)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Screenshot not found")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusNotFound, "not_found", "Screenshot not found")
		return
	}
	w.Header().Set("Cache-Control", "private, max-age=300")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}

func (s *Server) handlePlugins(w http.ResponseWriter, r *http.Request, sess store.Session) {
	state, err := s.site(r.Context()).PluginState(r.Context())
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, state)
}

type pluginAddRequest struct {
	Path    string `json:"path"`
	Version string `json:"version"`
}

func (s *Server) handlePluginAdd(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req pluginAddRequest
	if !decodeJSON(w, r, &req, 64*1024) {
		return
	}
	result, err := s.site(r.Context()).AddPlugin(r.Context(), req.Path, req.Version)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, result)
}

func (s *Server) handlePluginRemove(w http.ResponseWriter, r *http.Request, sess store.Session) {
	path := strings.TrimSpace(r.URL.Query().Get("path"))
	if path == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Module path is required")
		return
	}
	result, err := s.site(r.Context()).RemovePlugin(r.Context(), path)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) handlePluginTidy(w http.ResponseWriter, r *http.Request, sess store.Session) {
	result, err := s.site(r.Context()).TidyPlugins(r.Context())
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, result)
}
