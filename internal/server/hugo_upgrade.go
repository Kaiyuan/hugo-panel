package server

import (
	"context"
	"net/http"
	"strings"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

type hugoUpgradeRequest struct {
	Version string `json:"version"`
}

// handleHugoUpgrade downloads an official extended Hugo release, checks the
// active themes and modules with the new binary, and only then switches the
// configured binary. The download lives under DATA_DIR because the system
// Hugo binary in the container image is owned by root.
func (s *Server) handleHugoUpgrade(w http.ResponseWriter, r *http.Request, sess store.Session) {
	var req hugoUpgradeRequest
	if !decodeJSON(w, r, &req, 32*1024) {
		return
	}
	if !s.upgradeMu.TryLock() {
		writeError(w, http.StatusConflict, "upgrade_running", "A Hugo upgrade is already running")
		return
	}
	defer s.upgradeMu.Unlock()

	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Minute)
	defer cancel()
	site := s.site(ctx)
	current, _ := site.HugoVersion(ctx)
	version := strings.TrimPrefix(strings.TrimSpace(req.Version), "v")
	if version == "" {
		version = s.hugo.LatestHugoVersion(ctx)
	}
	if version == "" {
		writeError(w, http.StatusServiceUnavailable, "latest_unavailable", "Could not determine the latest Hugo version. Enter a version to install.")
		return
	}
	if current != "" && hugo.HugoVersionAtLeast(current, version) {
		writeJSON(w, http.StatusOK, map[string]any{
			"ok":         true,
			"up_to_date": true,
			"version":    current,
			"binary":     site.Binary,
			"hugo_dir":   site.Dir,
			"checked_at": time.Now().UTC().Format(time.RFC3339),
		})
		return
	}

	binaryPath, err := s.hugo.InstallHugoVersion(ctx, s.cfg.DataDir, version)
	if err != nil {
		writeError(w, http.StatusBadGateway, "hugo_download_failed", err.Error())
		return
	}
	newSite := *site
	newSite.Binary = binaryPath
	check, err := newSite.CheckUpgradeCompatibility(ctx, binaryPath)
	if err != nil {
		writeError(w, http.StatusBadGateway, "hugo_check_failed", "Could not test the new Hugo binary: "+err.Error())
		return
	}
	if !check.Compatible {
		writeJSON(w, http.StatusConflict, map[string]any{
			"error":         "incompatible",
			"message":       "Hugo was not upgraded because the active themes or modules are not compatible: " + check.Reason,
			"compatibility": check,
		})
		return
	}
	if err := s.settings.Update(ctx, map[string]string{settings.KeyHugoBinary: binaryPath}); err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not save the new Hugo binary path")
		return
	}
	s.hugo.ClearVersionCache()
	writeJSON(w, http.StatusOK, map[string]any{
		"ok":               true,
		"upgraded":         true,
		"up_to_date":       false,
		"version":          check.HugoVersion,
		"previous_version": current,
		"binary":           binaryPath,
		"hugo_dir":         site.Dir,
		"compatibility":    check,
	})
}
