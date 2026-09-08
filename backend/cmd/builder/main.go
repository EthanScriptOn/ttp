package main

import (
	"context"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"github.com/yuebuy/cicd-platform/backend/internal/builder"
)

func main() {
	slog.SetDefault(slog.New(slog.NewJSONHandler(os.Stdout, &slog.HandlerOptions{Level: slog.LevelInfo})))
	config := builder.LoadRuntimeConfig()
	executor, err := config.Executor()
	if err != nil {
		slog.Error("invalid builder configuration", "error", err)
		os.Exit(1)
	}
	service, err := builder.NewServer(config.Token, executor, config.MaxConcurrentBuilds)
	if err != nil {
		slog.Error("configure builder", "error", err)
		os.Exit(1)
	}
	server := &http.Server{Addr: config.Addr, Handler: service.Handler(), ReadHeaderTimeout: 5 * time.Second, ReadTimeout: 15 * time.Second, WriteTimeout: config.BuildTimeout + time.Minute, IdleTimeout: 60 * time.Second}
	go func() {
		slog.Info("ttp builder listening", "address", config.Addr, "max_concurrent_builds", config.MaxConcurrentBuilds)
		if err := server.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("serve builder", "error", err)
			os.Exit(1)
		}
	}()
	stop := make(chan os.Signal, 1)
	signal.Notify(stop, syscall.SIGINT, syscall.SIGTERM)
	<-stop
	shutdown, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	if err := server.Shutdown(shutdown); err != nil {
		slog.Error("shutdown builder", "error", err)
	}
}
