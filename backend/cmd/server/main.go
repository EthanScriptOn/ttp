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
	"github.com/yuebuy/cicd-platform/backend/internal/imagebuild"
	"github.com/yuebuy/cicd-platform/backend/internal/release"
	"github.com/yuebuy/cicd-platform/backend/internal/runtime"
	"github.com/yuebuy/cicd-platform/backend/internal/store"
)

func main() {
	logHandler := slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})
	slog.SetDefault(slog.New(logHandler))
	cfg := config.Load()
	if err := cfg.Validate(); err != nil {
		slog.Error("invalid server configuration", "error", err)
		os.Exit(1)
	}
	ctx := context.Background()
	dataStore, err := store.Open(ctx, cfg.MySQLDSN)
	if err != nil {
		slog.Error("open store", "error", err)
		os.Exit(1)
	}
	defer dataStore.Close()

	gitProvider, err := git.NewRegistry(git.RegistryConfig{
		Provider:     cfg.GitProvider,
		APIBaseURL:   cfg.GitAPIBaseURL,
		AllowedHosts: cfg.GitHosts,
		Timeout:      cfg.GitTimeout,
	})
	if err != nil {
		slog.Error("configure git provider", "error", err)
		os.Exit(1)
	}
	runtimeProvider, err := newRuntimeProvider(cfg)
	if err != nil {
		slog.Error("configure runtime provider", "error", err)
		os.Exit(1)
	}
	releaseRepository, ok := dataStore.(release.Repository)
	if !ok {
		slog.Error("configure release repository", "error", "durable store does not implement release persistence")
		os.Exit(1)
	}
	releaseService, err := release.NewPersistentService(ctx, gitProvider, releaseRepository)
	if err != nil {
		slog.Error("load releases", "error", err)
		os.Exit(1)
	}
	var imageBuilder imagebuild.Builder
	if cfg.ImageBuilderURL != "" || cfg.ImageBuilderToken != "" {
		remoteBuilder, builderErr := imagebuild.NewRemote(imagebuild.RemoteConfig{URL: cfg.ImageBuilderURL, Token: cfg.ImageBuilderToken, Timeout: cfg.ImageBuilderTimeout})
		if builderErr != nil {
			slog.Error("configure image builder", "error", builderErr)
			os.Exit(1)
		}
		imageBuilder = remoteBuilder
	}
	deps := api.Dependencies{Config: cfg, Store: dataStore, Auth: auth.NewManager(cfg.JWTSecret, cfg.JWTMinutes), Git: gitProvider, Release: releaseService, Runtime: runtime.NewService(runtimeProvider), ImageBuilder: imageBuilder, CredentialKey: cfg.GitCredentialKey}
	server := &http.Server{Addr: cfg.Addr, Handler: api.New(deps).Router(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: 30 * time.Second, IdleTimeout: 60 * time.Second}

	go func() {
		slog.Info("cicd platform listening", "address", cfg.Addr, "runtime_provider", cfg.RuntimeProvider, "image_builder_configured", imageBuilder != nil)
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
