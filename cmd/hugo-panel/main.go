package main

import (
	"context"
	"errors"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"
	"time"

	"github.com/Kaiyuan/hugo-panel/internal/config"
	"github.com/Kaiyuan/hugo-panel/internal/hugo"
	"github.com/Kaiyuan/hugo-panel/internal/i18n"
	"github.com/Kaiyuan/hugo-panel/internal/mailer"
	"github.com/Kaiyuan/hugo-panel/internal/server"
	"github.com/Kaiyuan/hugo-panel/internal/settings"
	"github.com/Kaiyuan/hugo-panel/internal/store"
	"github.com/Kaiyuan/hugo-panel/web"
)

var (
	version   = "0.0.1"
	commit    = "none"
	buildTime = "unknown"
)

func main() {
	if err := run(); err != nil {
		fmt.Fprintln(os.Stderr, "hugo-panel:", err)
		os.Exit(1)
	}
}

func run() error {
	cfg, err := config.Load()
	if err != nil {
		return err
	}
	cfg.Version = version

	level := slog.LevelInfo
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn", "warning":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	}
	logger := slog.New(slog.NewTextHandler(os.Stdout, &slog.HandlerOptions{Level: level}))
	slog.SetDefault(logger)

	if err := os.MkdirAll(cfg.DataDir, 0o750); err != nil {
		return fmt.Errorf("create data directory: %w", err)
	}
	_ = os.Chmod(cfg.DataDir, 0o750)
	localeDir := filepath.Join(cfg.DataDir, "locales")
	if err := os.MkdirAll(localeDir, 0o750); err != nil {
		return fmt.Errorf("create locale directory: %w", err)
	}

	ctx := context.Background()
	st, err := store.Open(ctx, filepath.Join(cfg.DataDir, "hugo-panel.db"))
	if err != nil {
		return err
	}
	defer st.Close()

	set := settings.New(st, cfg)
	if err := set.EnsureDefaults(ctx); err != nil {
		return err
	}
	localeFS, err := fs.Sub(web.Files, "locales")
	if err != nil {
		return err
	}
	tr := i18n.New(localeFS, cfg.DataDir)
	hs := hugo.NewService()
	if raw, err := set.Get(ctx, settings.KeyLastBuild); err == nil {
		hs.LoadPersistedBuild(raw)
	}
	srv, err := server.New(cfg, st, set, tr, mailer.New(), hs, logger, version)
	if err != nil {
		return err
	}

	httpServer := &http.Server{
		Addr:              cfg.ListenAddr(),
		Handler:           srv.Handler(),
		ReadHeaderTimeout: 10 * time.Second,
		ReadTimeout:       5 * time.Minute,
		WriteTimeout:      25 * time.Minute,
		IdleTimeout:       2 * time.Minute,
		MaxHeaderBytes:    1 << 20,
	}

	cleanupCtx, cleanupCancel := context.WithCancel(context.Background())
	defer cleanupCancel()
	go func() {
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ticker.C:
				if err := st.Cleanup(cleanupCtx); err != nil {
					logger.Warn("cleanup sessions", "error", err)
				}
			case <-cleanupCtx.Done():
				return
			}
		}
	}()

	errCh := make(chan error, 1)
	go func() {
		logger.Info("Hugo Panel starting",
			"version", version,
			"commit", commit,
			"built", buildTime,
			"addr", cfg.ListenAddr(),
			"data_dir", cfg.DataDir,
			"hugo_dir", set.HugoDir(ctx),
		)
		if err := httpServer.ListenAndServe(); err != nil && !errors.Is(err, http.ErrServerClosed) {
			errCh <- err
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	select {
	case sig := <-stop:
		logger.Info("shutting down", "signal", sig.String())
	case err := <-errCh:
		return err
	}

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	if err := httpServer.Shutdown(shutdownCtx); err != nil {
		return fmt.Errorf("shutdown: %w", err)
	}
	return nil
}
