package server

import (
	"net/http"
)

func (s *Server) handleLanguages(w http.ResponseWriter, r *http.Request) {
	languages, err := s.i18n.Languages()
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load languages")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{"languages": languages})
}

func (s *Server) handleMessages(w http.ResponseWriter, r *http.Request) {
	code := r.PathValue("code")
	if code == "" {
		writeError(w, http.StatusBadRequest, "invalid_locale", "Locale code is required")
		return
	}
	messages, err := s.i18n.Messages(code)
	if err != nil {
		writeError(w, http.StatusBadRequest, "invalid_locale", err.Error())
		return
	}
	w.Header().Set("Cache-Control", "no-cache")
	writeJSON(w, http.StatusOK, map[string]any{"code": code, "messages": messages})
}
