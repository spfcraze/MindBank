package main

import (
	"context"
	"log/slog"
	"os"
	"os/signal"
	"syscall"

	"mindbank/internal/config"
	"mindbank/internal/db"
	"mindbank/internal/embedder"
	mcpsrv "mindbank/internal/mcp"
)

func main() {
	cfg := config.Load()

	// Log to stderr only (stdout is for MCP protocol)
	slog.SetDefault(slog.New(slog.NewTextHandler(os.Stderr, &slog.HandlerOptions{Level: slog.LevelWarn})))

	// Connect to database
	pool, err := db.Connect(cfg.DBDSN)
	if err != nil {
		slog.Error("failed to connect to database", "error", err)
		os.Exit(1)
	}
	defer pool.Close()

	// Run migrations
	if err := db.Migrate(context.Background(), pool); err != nil {
		slog.Error("failed to run migrations", "error", err)
		os.Exit(1)
	}

	// Embedder client
	embClient := embedder.NewClient(cfg.OllamaURL, cfg.EmbedModel)

	// Create MCP server
	server := mcpsrv.NewServer(pool, embClient)

	// Graceful shutdown
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	sigCh := make(chan os.Signal, 1)
	signal.Notify(sigCh, syscall.SIGINT, syscall.SIGTERM)
	go func() {
		<-sigCh
		cancel()
	}()

	// Check transport mode
	transport := os.Getenv("MCP_TRANSPORT")
	if transport == "" {
		// Check command line args
		for i, arg := range os.Args {
			if arg == "--transport" && i+1 < len(os.Args) {
				transport = os.Args[i+1]
				break
			}
			if arg == "--http" {
				transport = "http"
				break
			}
		}
	}

	if transport == "http" {
		// HTTP transport mode
		port := os.Getenv("MCP_HTTP_PORT")
		if port == "" {
			for i, arg := range os.Args {
				if arg == "--http-port" && i+1 < len(os.Args) {
					port = os.Args[i+1]
					break
				}
			}
		}
		if port == "" {
			port = "8096"
		}

		httpServer, err := mcpsrv.NewHTTPServer(server, port)
		if err != nil {
			slog.Error("failed to create http server", "error", err)
			os.Exit(1)
		}

		if err := httpServer.Run(ctx); err != nil {
			slog.Error("http server error", "error", err)
			os.Exit(1)
		}
	} else {
		// Stdio transport mode (default)
		server.Run(ctx)
	}
}
