package main

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"os"
	"os/signal"
	"syscall"
	"time"

	"mindbank/internal/autocapture"
	"mindbank/internal/config"
	"mindbank/internal/db"
	"mindbank/internal/embedder"
	"mindbank/internal/handler"
	"mindbank/internal/repository"
)

func main() {
	cfg := config.Load()

	// Setup structured logging
	var level slog.Level
	switch cfg.LogLevel {
	case "debug":
		level = slog.LevelDebug
	case "warn":
		level = slog.LevelWarn
	case "error":
		level = slog.LevelError
	default:
		level = slog.LevelInfo
	}
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: level})))

	slog.Info("mindbank starting", "port", cfg.Port)

	// Security warning: auth disabled by default
	if os.Getenv("MB_API_KEY") == "" {
		slog.Warn("API authentication is disabled — set MB_API_KEY to enable auth in production")
	}

	// Connect to database
	pool, err := db.Connect(cfg.DBDSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()
	slog.Info("database connected")

	// Run migrations
	if err := db.Migrate(context.Background(), pool); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}
	slog.Info("migrations complete")

	// Purge old temporal versions on startup (reduces query overhead)
	nodeRepo := repository.NewNodeRepo(pool)
	purged, err := nodeRepo.PurgeOldVersions(context.Background(), 7)
	if err != nil {
		slog.Warn("startup purge failed", "error", err)
	} else if purged > 0 {
		slog.Info("purged old temporal versions", "count", purged)
	}

	// Warm up Ollama embedding model (eliminates 370ms cold start on first request)
	embClient := embedder.NewClient(cfg.OllamaURL, cfg.EmbedModel)
	go func() {
		ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
		defer cancel()
		if _, err := embClient.Embed(ctx, "warmup"); err != nil {
			slog.Warn("ollama warmup failed (embeddings will be slow on first request)", "error", err)
		} else {
			slog.Info("ollama embedding model warmed up")
		}
	}()

	// Setup router
	router := handler.NewRouter(pool, cfg)

	// Start namespace-aware auto-capture watcher
	homeDir, err := os.UserHomeDir()
	if err != nil {
		slog.Warn("failed to get home dir for auto-capture", "error", err)
	} else {
		watchPath := homeDir + "/.hermes/sessions"
		watcher := autocapture.NewWatcher(nodeRepo, watchPath)
		if err := watcher.Start(context.Background()); err != nil {
			slog.Warn("failed to start auto-capture watcher", "error", err)
		} else {
			slog.Info("auto-capture watcher started", "path", watchPath)
			// Start background scanning for new files
			go watcher.Watch(context.Background())
		}
	}

	// HTTP server
	addr := fmt.Sprintf(":%d", cfg.Port)
	srv := &http.Server{
		Addr:         addr,
		Handler:      router,
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
		IdleTimeout:  120 * time.Second,
	}

	// Graceful shutdown
	done := make(chan os.Signal, 1)
	signal.Notify(done, syscall.SIGINT, syscall.SIGTERM)

	go func() {
		slog.Info("server listening", "addr", addr)
		if err := srv.ListenAndServe(); err != nil && err != http.ErrServerClosed {
			slog.Error("server error", "error", err)
			os.Exit(1)
		}
	}()

	<-done
	slog.Info("shutting down...")

	ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
	defer cancel()

	if err := srv.Shutdown(ctx); err != nil {
		slog.Error("shutdown error", "error", err)
	}

	slog.Info("mindbank stopped")
}
