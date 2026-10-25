package server

import (
	"context"
	"errors"
	"fmt"
	"html"
	"net/http"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
)

func (s *Server) handleBuildGet(w http.ResponseWriter, r *http.Request, sess store.Session) {
	writeJSON(w, http.StatusOK, map[string]any{"build": s.hugo.LastBuild()})
}

func (s *Server) handleBuildRun(w http.ResponseWriter, r *http.Request, sess store.Session) {
	ctx := r.Context()
	site := s.site(ctx)
	result, err := s.hugo.Build(ctx, site)
	if err != nil {
		if errors.Is(err, hugo.ErrBuildRunning) {
			writeError(w, http.StatusConflict, "build_running", "A build is already running")
			return
		}
		writeError(w, http.StatusInternalServerError, "build_failed", err.Error())
		return
	}
	_ = s.settings.Update(ctx, map[string]string{settings.KeyLastBuild: s.hugo.MarshalLastBuild()})
	if !result.Success {
		go s.notifyBuildFailure(site, result)
	}
	writeJSON(w, http.StatusOK, result)
}

func (s *Server) notifyBuildFailure(site *hugo.Site, result *hugo.BuildResult) {
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Minute)
	defer cancel()
	if !s.settings.Bool(ctx, settings.KeyNotifications) {
		return
	}
	smtpCfg, err := s.settings.EffectiveSMTP(ctx)
	if err != nil || !smtpCfg.Configured() {
		return
	}
	users, err := s.store.ListUsersWithEmail(ctx)
	if err != nil {
		return
	}
	cfg, _ := site.LoadConfig()
	title := cfg.SiteTitle
	if title == "" {
		title = "Hugo site"
	}
	output := result.Output
	if len([]rune(output)) > 6000 {
		output = string([]rune(output)[len([]rune(output))-6000:])
	}
	subject := "Hugo build failed: " + title
	text := fmt.Sprintf("A build of %s failed at %s.\n\n%s\n", title, result.FinishedAt, output)
	htmlBody := fmt.Sprintf(`<!doctype html><html><body style="margin:0;background:#f6f7f9;padding:28px;font-family:-apple-system,BlinkMacSystemFont,'Segoe UI',sans-serif;color:#111">
<table role="presentation" width="100%%"><tr><td align="center"><table role="presentation" width="620" style="background:#fff;border:1px solid #e7e9ee;border-radius:16px;padding:30px">
<tr><td><div style="font-size:13px;font-weight:700;letter-spacing:.08em;color:#b91c1c">BUILD FAILED</div>
<h1 style="font-size:24px;margin:10px 0 8px">%s</h1>
<p style="color:#4b5563;font-size:15px;margin:0 0 18px">The build finished at %s. Review the output below in Hugo Panel.</p>
<pre style="white-space:pre-wrap;word-break:break-word;background:#0b0b0c;color:#e5e7eb;border-radius:12px;padding:16px;font-size:13px">%s</pre>
</td></tr></table></td></tr></table></body></html>`,
		html.EscapeString(title), html.EscapeString(result.FinishedAt), html.EscapeString(output))
	for _, user := range users {
		msg := mailer.Message{To: user.Email, Subject: subject, Text: text, HTML: htmlBody}
		if err := s.mailer.Send(ctx, smtpCfg, msg); err != nil {
			s.logger.Error("send build failure notification", "error", err, "to", user.Email)
		}
	}
}
