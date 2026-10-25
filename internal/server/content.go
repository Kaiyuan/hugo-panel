package server

import (
	"net/http"
	"strconv"
	"strings"

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
