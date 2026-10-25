package server

import (
	"errors"
	"io"
	"net/http"
	"os"
	"strings"

	"github.com/Kaiyuan/hugo-panel/internal/store"
)

func (s *Server) handleMediaList(w http.ResponseWriter, r *http.Request, sess store.Session) {
	site := s.site(r.Context())
	files, err := site.ListMedia()
	if err != nil {
		writeHugoError(w, err)
		return
	}
	stats, _ := site.MediaStats()
	writeJSON(w, http.StatusOK, map[string]any{"files": files, "stats": stats, "root": "static/images"})
}

func (s *Server) handleMediaUpload(w http.ResponseWriter, r *http.Request, sess store.Session) {
	r.Body = http.MaxBytesReader(w, r.Body, 26*1024*1024)
	if err := r.ParseMultipartForm(26 * 1024 * 1024); err != nil {
		writeError(w, http.StatusBadRequest, "file_too_large", "Upload is too large (maximum 20 MB)")
		return
	}
	defer func() {
		if r.MultipartForm != nil {
			_ = r.MultipartForm.RemoveAll()
		}
	}()
	file, header, err := r.FormFile("file")
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_request", "Choose an image to upload")
		return
	}
	defer file.Close()
	if header.Size > 20*1024*1024 {
		writeError(w, http.StatusBadRequest, "file_too_large", "Upload is too large (maximum 20 MB)")
		return
	}
	subdir := r.FormValue("dir")
	media, err := s.site(r.Context()).SaveMedia(io.LimitReader(file, 20*1024*1024), header.Filename, subdir)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusCreated, map[string]any{"file": media})
}

func (s *Server) handleMediaDelete(w http.ResponseWriter, r *http.Request, sess store.Session) {
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "File path is required")
		return
	}
	if err := s.site(r.Context()).DeleteMedia(path); err != nil {
		writeHugoError(w, err)
		return
	}
	writeJSON(w, http.StatusOK, map[string]bool{"ok": true})
}

func (s *Server) handleMediaFile(w http.ResponseWriter, r *http.Request, sess store.Session) {
	path := r.URL.Query().Get("path")
	if strings.TrimSpace(path) == "" {
		writeError(w, http.StatusBadRequest, "invalid_request", "File path is required")
		return
	}
	full, ctype, err := s.site(r.Context()).MediaPath(path)
	if err != nil {
		writeHugoError(w, err)
		return
	}
	f, err := os.Open(full)
	if err != nil {
		if errors.Is(err, os.ErrNotExist) {
			writeError(w, http.StatusNotFound, "not_found", "File not found")
			return
		}
		writeError(w, http.StatusInternalServerError, "server_error", "Could not read file")
		return
	}
	defer f.Close()
	info, err := f.Stat()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not read file")
		return
	}
	w.Header().Set("Content-Type", ctype)
	w.Header().Set("Cache-Control", "private, max-age=300")
	w.Header().Set("Content-Security-Policy", "sandbox")
	http.ServeContent(w, r, info.Name(), info.ModTime(), f)
}
