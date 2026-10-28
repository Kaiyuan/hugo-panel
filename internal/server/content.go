package server

import (
	"context"
	"net/http"
	"strconv"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/ai"
	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

func (s *Server) handleContentList(w http.ResponseWriter, r *http.Request, sess store.Session) {
	q := r.URL.Query()
	page, _ := strconv.Atoi(q.Get("page"))
	limit, _ := strconv.Atoi(q.Get("limit"))
	site := s.site(r.Context())
	res, err := site.ListContent(hugo.ListOptions{
		Section: strings.TrimSpace(q.Get("section")),
		Status:  strings.TrimSpace(q.Get("status")),
		Query:   strings.TrimSpace(q.Get("q")),
		Page:    page,
		Limit:   limit,
	})
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, res)
}

func (s *Server) handleContentMeta(w http.ResponseWriter, r *http.Request, sess store.Session) {
	meta, err := s.site(r.Context()).ContentMeta()
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, meta)
}

type contentSlugRequest struct {
	Title string `json:"title"`
}

// handleContentSlug turns an article title into an English file name. A
// dedicated translation provider is tried first, then the configured AI
// provider, and finally a local ASCII slug so the editor always works.
func (s *Server) handleContentSlug(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req contentSlugRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	title := strings.TrimSpace(req.Title)
	if title == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Title is required")
		return
	}
	if len([]rune(title)) > 300 {
		writeError(w, http.StatusBadRequest, "invalid_request", "Title is too long")
		return
	}
	trCfg, err := s.settings.Translation(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load translation settings")
		return
	}
	aiCfg, err := s.settings.AI(r.Context())
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load AI settings")
		return
	}
	slug := hugo.Slugify(title)
	source := ""
	reason := ""
	if trCfg.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 20*time.Second)
		defer cancel()
		out, err := s.translator.Translate(ctx, trCfg, title, "en")
		if err != nil {
			s.logger.Warn("translate title", "provider", trCfg.Provider, "error", err)
			reason = "provider_failed"
		} else if translatedSlug := hugo.Slugify(out); translatedSlug != "" {
			slug = translatedSlug
			source = "translate"
		}
	} else {
		reason = "not_configured"
	}
	if source == "" && aiCfg.Configured() {
		ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
		out, err := s.aiClient.Complete(ctx, aiCfg, []ai.Message{
			{Role: "system", Content: aiTitleSystemPrompt},
			{Role: "user", Content: title},
		}, 80, 0.2)
		cancel()
		if err != nil {
			s.logger.Warn("ai translate title", "model", aiCfg.Model, "error", err)
			reason = "ai_failed"
		} else if translatedSlug := hugo.Slugify(stripMarkdownFence(out)); translatedSlug != "" {
			slug = translatedSlug
			source = "ai"
			reason = ""
		}
	}
	if source == "" {
		if slug == "" {
			slug = "post-" + time.Now().Format("20060102-150405")
		}
		if reason == "" {
			reason = "not_configured"
		}
	}
	slug = truncateSlug(slug, 80)
	writeJSON(w, http.StatusOK, map[string]any{
		"slug":       slug,
		"translated": source != "",
		"source":     source,
		"reason":     reason,
	})
}

// truncateSlug keeps generated file names within a predictable length.
func truncateSlug(slug string, limit int) string {
	if len(slug) <= limit {
		return slug
	}
	return strings.Trim(slug[:limit], "-")
}

type contentDirectoryRequest struct {
	Path string `json:"path"`
}

// handleContentDirectory creates a folder inside content/ so new articles can
// be filed immediately, before a file exists.
func (s *Server) handleContentDirectory(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req contentDirectoryRequest
	if !decodeJSON(w, r, &req, 8*1024) {
		return
	}
	dir, err := s.site(r.Context()).CreateContentDirectory(req.Path)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]string{"path": dir})
}

func (s *Server) handleContentGet(w http.ResponseWriter, r *http.Request, sess store.Session) {
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Content path is required")
		return
	}
	item, err := s.site(r.Context()).ReadContent(path)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentCreate(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var in hugo.SaveInput
	if !decodeJSON(w, r, &in, 24*1024*1024) {
		return
	}
	item, err := s.site(r.Context()).CreateContent(in)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, item)
}

func (s *Server) handleContentUpdate(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var in hugo.SaveInput
	if !decodeJSON(w, r, &in, 24*1024*1024) {
		return
	}
	item, err := s.site(r.Context()).UpdateContent(in)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, item)
}

func (s *Server) handleContentDelete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "Content path is required")
		return
	}
	if err := s.site(r.Context()).DeleteContent(path); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}
