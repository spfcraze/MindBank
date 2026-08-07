package mcp

import (
	"context"
	"strings"
	"encoding/json"
	"fmt"
	"log/slog"
	"net"
	"net/http"
	"time"
)

// HTTPServer wraps the MCP server with an HTTP transport.
type HTTPServer struct {
	mcpServer *Server
	httpServer *http.Server
	listener   net.Listener
}

// NewHTTPServer creates an HTTP transport for the MCP server.
func NewHTTPServer(mcpServer *Server, port string) (*HTTPServer, error) {
	if port == "" {
		port = "8096"
	}

	mux := http.NewServeMux()
	hs := &HTTPServer{
		mcpServer: mcpServer,
	}

	// MCP JSON-RPC endpoint
	mux.HandleFunc("/mcp", hs.handleMCP)
	mux.HandleFunc("/mcp/", hs.handleMCP)

	// Health check
	mux.HandleFunc("/health", hs.handleHealth)

	hs.httpServer = &http.Server{
		Addr:         ":" + port,
		Handler:      NewRateLimiter(100, 10*time.Second).Middleware(mux),
		ReadTimeout:  30 * time.Second,
		WriteTimeout: 30 * time.Second,
	}

	listener, err := net.Listen("tcp", hs.httpServer.Addr)
	if err != nil {
		return nil, fmt.Errorf("failed to listen on %s: %w", hs.httpServer.Addr, err)
	}
	hs.listener = listener

	return hs, nil
}

// Run starts the HTTP server.
func (hs *HTTPServer) Run(ctx context.Context) error {
	slog.Info("mcp http server started", "addr", hs.httpServer.Addr)

	// Run server in background
	go func() {
		if err := hs.httpServer.Serve(hs.listener); err != nil && err != http.ErrServerClosed {
			slog.Error("http server error", "error", err)
		}
	}()

	// Wait for shutdown signal
	<-ctx.Done()

	shutdownCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	return hs.httpServer.Shutdown(shutdownCtx)
}

// Port returns the actual port the server is listening on.
func (hs *HTTPServer) Port() int {
	if hs.listener == nil {
		return 0
	}
	return hs.listener.Addr().(*net.TCPAddr).Port
}

// handleMCP handles JSON-RPC MCP requests over HTTP.
func (hs *HTTPServer) handleMCP(w http.ResponseWriter, r *http.Request) {
	if r.Method != http.MethodPost {
		http.Error(w, "Method not allowed", http.StatusMethodNotAllowed)
		return
	}

	var req MCPRequest
	if err := json.NewDecoder(r.Body).Decode(&req); err != nil {
		http.Error(w, fmt.Sprintf(`{"jsonrpc":"2.0","error":{"code":-32700,"message":"Parse error"}}`), http.StatusBadRequest)
		return
	}
	defer r.Body.Close()

	// Bound each request so one hung DB/Ollama call can't tie up the handler
	// indefinitely (mirrors the 60s cap the stdio transport applies).
	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
	defer cancel()

	// Transport-level client context: HTTP headers override the auto-derived
	// namespace (e.g. a client that knows its project pins it per request).
	// Header values come from Hermes config interpolation; an unresolved
	// "${VAR}" placeholder or empty value must be treated as absent.
	clean := func(v string) string {
		v = strings.TrimSpace(v)
		if v == "" || strings.Contains(v, "${") {
			return ""
		}
		return v
	}
	ctx = withClientCtx(ctx,
		clean(r.Header.Get("X-Mindbank-Workspace")),
		clean(r.Header.Get("X-Mindbank-Namespace")),
		clean(r.Header.Get("X-Mindbank-Session")))

	resp := hs.mcpServer.handleRequest(ctx, &req)
	if resp == nil {
		// Notification — no response
		w.WriteHeader(http.StatusAccepted)
		return
	}

	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(http.StatusOK)
	if err := json.NewEncoder(w).Encode(resp); err != nil {
		slog.Error("encode http response", "error", err)
	}
}

// handleHealth returns server health status.
func (hs *HTTPServer) handleHealth(w http.ResponseWriter, r *http.Request) {
	w.Header().Set("Content-Type", "application/json")
	json.NewEncoder(w).Encode(map[string]any{
		"status":    "ok",
		"transport": "http",
		"version":   "0.1.0",
	})
}
