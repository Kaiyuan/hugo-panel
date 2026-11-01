package server

import (
	"context"
	"errors"
	"net/http"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/deploy"
	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

// buildResponse keeps the existing build JSON contract while adding the
// deployment that followed a successful build.
type buildResponse struct {
	*hugo.BuildResult
	Deploy *deploy.Result `json:"deploy,omitempty"`
}

func (s *Server) handleDeployGet(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	cfg, err := s.settings.Deploy(ctx, s.settings.HugoDir(ctx))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load deployment settings")
		return
	}
	writeJSON(w, http.StatusOK, map[string]any{
		"deploy":     s.deployer.Last(),
		"enabled":    cfg.Enabled,
		"configured": cfg.Validate() == nil,
	})
}

func (s *Server) handleDeployRun(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx, cancel := context.WithTimeout(r.Context(), 25*time.Minute)
	defer cancel()
	cfg, err := s.settings.Deploy(ctx, s.settings.HugoDir(ctx))
	if err != nil {
		writeError(w, http.StatusInternalServerError, "server_error", "Could not load deployment settings")
		return
	}
	if err := cfg.Validate(); err != nil {
		writeError(w, http.StatusBadRequest, "deploy_not_configured", err.Error())
		return
	}
	result, err := s.deployer.Run(ctx, cfg)
	if err != nil {
		if errors.Is(err, deploy.ErrRunning) {
			writeError(w, http.StatusConflict, "deploy_running", "A deployment is already running")
			return
		}
		writeError(w, http.StatusInternalServerError, "deploy_failed", err.Error())
		return
	}
	s.recordDeploy(result)
	writeJSON(w, http.StatusOK, result)
}

// deployAfterBuild runs rsync once a Hugo build has succeeded. Deployments are
// best effort: a failed copy never turns a successful build into a failure.
func (s *Server) deployAfterBuild(ctx context.Context) *deploy.Result {
	cfg, err := s.settings.Deploy(ctx, s.settings.HugoDir(ctx))
	if err != nil {
		s.logger.Error("load deployment settings", "error", err)
		return nil
	}
	if !cfg.Enabled {
		return nil
	}
	result, err := s.deployer.Run(ctx, cfg)
	if err != nil {
		if errors.Is(err, deploy.ErrRunning) {
			return &deploy.Result{Status: "failed", ExitCode: -1, Output: "A deployment is already running."}
		}
		s.logger.Error("deploy after build", "error", err)
		return nil
	}
	s.recordDeploy(result)
	if !result.Success {
		s.logger.Error("deployment after build failed", "output", result.Output)
	}
	return result
}

func (s *Server) recordDeploy(result *deploy.Result) {
	if result == nil {
		return
	}
	raw := s.deployer.MarshalLast()
	if raw == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := s.settings.Update(ctx, map[string]string{settings.KeyLastDeploy: raw}); err != nil {
		s.logger.Error("save deployment result", "error", err)
	}
}
