package server

import (
	"net/http"

	"github.com/Kaiyuan/hugo-panel/internal/release"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

// handlePanelVersion reports the newest published Hugo Panel release. The
// answer is cached by the checker, so the sidebar can call it on every load.
func (s *Server) handlePanelVersion(w http.ResponseWriter, r *http.Request, sess store.Session) {
	latest := s.updates.Latest(r.Context())
	writeJSON(w, http.StatusOK, map[string]any{
		"version":          s.version,
		"latest_version":   latest,
		"update_available": release.IsNewer(latest, s.version),
		"release_url":      release.PageURL,
	})
}
