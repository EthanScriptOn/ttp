package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/api"
	"github.com/yuebuy/cicd-platform/backend/internal/auth"
	"github.com/yuebuy/cicd-platform/backend/internal/config"
	"github.com/yuebuy/cicd-platform/backend/internal/git"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func main() {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(logHandler))
	cfg := config.Load()
	ctx := context.Background()
	dataStore, err := store.Open(ctx, cfg.MySQLDSN, cfg.DemoMode, cfg.BootstrapAdminPassword, cfg.DemoAdminPassword)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()

	var gitProvider git.Provider
	if cfg.DemoMode {
		gitProvider = git.NewDemoProvider()
	} else {
		gitProvider, err = git.NewRegistry(git.RegistryConfig{
			Provider:     cfg.GitProvider,
			Token:        cfg.GitToken,
			APIBaseURL:   cfg.GitAPIBaseURL,
			AllowedHosts: cfg.GitHosts,
			Timeout:      cfg.GitTimeout,
			ServiceAccount: git.ServiceAccount{
				Username:    cfg.GitServiceUsername,
				DisplayName: cfg.GitServiceDisplayName,
				Email:       cfg.GitServiceEmail,
				Provider:    cfg.GitProvider,
				AuthMethod:  "token",
			},
		})
		if err != nil {
			slog.Error("configure git provider", "error", err)
			os.Exit(1)
		}
	}
	runtimeProvider, err := newRuntimeProvider(cfg)
	if err != nil {
		slog.Error("configure runtime provider", "error", err)
		os.Exit(1)
	}
	var releaseService *release.Service
	if persistence, ok := dataStore.(release.Persistence); ok {
		releaseService, err = release.NewPersistentService(gitProvider, persistence)
		if err != nil {
			slog.Error("restore release state", "error", err)
			os.Exit(1)
		}
	} else {
		releaseService = release.NewService(gitProvider)
	}
	deps := api.Dependencies{Config: cfg, Store: dataStore, Auth: auth.NewManager(cfg.JWTSecret, cfg.JWTMinutes), Git: gitProvider, Release: releaseService, Runtime: runtime.NewService(runtimeProvider)}
	server := &http.Server{Addr: cfg.Addr, Handler: api.New(deps).Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}

	go func() {
		slog.Info("cicd platform listening", "address", cfg.Addr, "demo_mode", cfg.DemoMode, "runtime_provider", cfg.RuntimeProvider)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve", "error", err)
			os.Exit(1)
		}
	}()

	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdownCtx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdownCtx); err != nil {
		slog.Error("shutdown", "error", err)
	}
}
