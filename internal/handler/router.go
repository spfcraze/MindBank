package handler

import (
	"context"
	"embed"
	"encoding/json"
	"fmt"
	"io/fs"
	"log/slog"
	"net/http"
	"os"
	"strings"
	"time"

	"mindbank/internal/config"
	"mindbank/internal/embedder"
	"mindbank/internal/reasoner"
	"mindbank/internal/repository"

	"github.com/go-chi/chi/v5"
	"github.com/go-chi/chi/v5/middleware"
	"github.com/go-chi/cors"
	"github.com/jackc/pgx/v5/pgxpool"
)

//go:embed static/*
var staticFS embed.FS

func NewRouter(pool *pgxpool.Pool, cfg config.Config) http.Handler {
	r := chi.NewRouter()

	// Middleware
	r.Use(middleware.RequestID)
	r.Use(middleware.RealIP)
	r.Use(middleware.Recoverer)
	r.Use(middleware.Timeout(30 * 1e9)) // 30s
	r.Use(cors.Handler(cors.Options{
		AllowedOrigins:   []string{"http://localhost:*", "http://127.0.0.1:*", "https://*.mindbank.local"},
		AllowedMethods:   []string{"GET", "POST", "PUT", "DELETE", "OPTIONS"},
		AllowedHeaders:   []string{"Accept", "Authorization", "Content-Type"},
		ExposedHeaders:   []string{"Link"},
		AllowCredentials: false,
		MaxAge:           300,
	}))

	// Repos
	nodeRepo := repository.NewNodeRepo(pool)
	edgeRepo := repository.NewEdgeRepo(pool)
	searchRepo := repository.NewSearchRepo(pool)
	sessionRepo := repository.NewSessionRepo(pool)
	snapshotRepo := repository.NewSnapshotRepo(pool)

	// Embedder client
	embClient := embedder.NewClient(cfg.OllamaURL, cfg.EmbedModel)

	// Start embedding worker in background
	worker := embedder.NewWorker(pool, embClient)
	go worker.Run(context.Background())

	// Reasoner
	ruleBased := reasoner.NewRuleBased(pool)

	// Handlers
	nh := &NodeHandler{repo: nodeRepo, pool: pool}
	eh := &EdgeHandler{repo: edgeRepo, nodeRepo: nodeRepo}
	sh := NewSearchHandler(searchRepo, embClient, edgeRepo)
	sessH := NewSessionHandler(sessionRepo, nodeRepo, ruleBased)
	askH := NewAskHandler(searchRepo, snapshotRepo, edgeRepo, embClient)
	bh := NewBatchHandler(nodeRepo, edgeRepo)
	uh := NewUpdateHandler()

	// Web UI — serve index.html at root, graph.html at /graph
	r.Get("/graph-view", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, _ := staticFS.ReadFile("static/graph.html")
		w.Write(data)
	})
	r.Get("/about", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, _ := staticFS.ReadFile("static/about.html")
		w.Write(data)
	})
	r.Get("/updates", func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "text/html")
		data, _ := staticFS.ReadFile("static/updates.html")
		w.Write(data)
	})
	staticSub, _ := fs.Sub(staticFS, "static")
	r.Handle("/*", http.FileServer(http.FS(staticSub)))

	r.Route("/api/v1", func(r chi.Router) {
		// Auth middleware (disabled if MB_API_KEY not set)
		r.Use(APIKeyAuth)

		// Rate limiting: 100 requests per minute per IP
		r.Use(NewRateLimiter(100, time.Minute).Middleware)

		// Health
		r.Get("/health", func(w http.ResponseWriter, r *http.Request) {
			if err := pool.Ping(r.Context()); err != nil {
				respondJSON(w, 503, map[string]string{"status": "error", "postgres": "disconnected"})
				return
			}
			embErr := embClient.Health(r.Context())
			ollamaStatus := "connected"
			if embErr != nil {
				ollamaStatus = "unavailable"
			}
			respondJSON(w, 200, map[string]string{
				"status":          "ok",
				"postgres":        "connected",
				"ollama":          ollamaStatus,
				"embedding_model": cfg.EmbedModel,
				"version":         getLocalVersion(),
			})
		})

		// Prometheus metrics
		r.Get("/metrics", func(w http.ResponseWriter, r *http.Request) {
			// Node counts by namespace
			rows, err := pool.Query(r.Context(), `SELECT namespace, COUNT(*) FROM nodes WHERE valid_to IS NULL GROUP BY namespace`)
			if err != nil {
				w.WriteHeader(500)
				return
			}
			defer rows.Close()
			w.Header().Set("Content-Type", "text/plain; version=0.0.4")
			fmt.Fprintf(w, "# HELP mindbank_nodes_total Total active nodes by namespace\n")
			fmt.Fprintf(w, "# TYPE mindbank_nodes_total gauge\n")
			for rows.Next() {
				var ns string
				var count int
				rows.Scan(&ns, &count)
				fmt.Fprintf(w, "mindbank_nodes_total{namespace=\"%s\"} %d\n", ns, count)
			}

			// Edge count
			var edgeCount int
			pool.QueryRow(r.Context(), `SELECT COUNT(*) FROM edges`).Scan(&edgeCount)
			fmt.Fprintf(w, "# HELP mindbank_edges_total Total edges\n")
			fmt.Fprintf(w, "# TYPE mindbank_edges_total gauge\n")
			fmt.Fprintf(w, "mindbank_edges_total %d\n", edgeCount)

			// Snapshot cache stats
			fmt.Fprintf(w, "# HELP mindbank_up Server is up\n")
			fmt.Fprintf(w, "# TYPE mindbank_up gauge\n")
			fmt.Fprintf(w, "mindbank_up 1\n")
		})

		// Nodes
		r.Route("/nodes", func(r chi.Router) {
			r.Post("/", nh.Create)
			r.Get("/", nh.List)
			r.Post("/dedup", nh.Dedup)
			r.Post("/recalculate", nh.Recalculate)
			r.Get("/{id}", nh.Get)
			r.Put("/{id}", nh.Update)
			r.Delete("/{id}", nh.Delete)
			r.Get("/{id}/neighbors", eh.GetNeighbors)
			r.Get("/{id}/path/{targetID}", eh.FindPath)
			r.Get("/{id}/history", nh.GetHistory)
		})

		// Edges
		r.Route("/edges", func(r chi.Router) {
			r.Post("/", eh.Create)
			r.Delete("/{id}", eh.Delete)
			r.Get("/", eh.List)
		})

		// Search
		RegisterSearchRoutes(r, sh)

		// Sessions
		RegisterSessionRoutes(r, sessH)

		// Ask + Snapshot
		RegisterAskRoutes(r, askH)

		// Batch + Export/Import + Purge
		RegisterBatchRoutes(r, bh)

		// Updates
		RegisterUpdateRoutes(r, uh)
	})

	return r
}

// respondJSON writes a JSON response.
func respondJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	if err := json.NewEncoder(w).Encode(data); err != nil {
		slog.Error("failed to encode response", "error", err)
	}
}

// getLocalVersion reads the VERSION file.
func getLocalVersion() string {
	data, err := os.ReadFile("VERSION")
	if err != nil {
		return "0.0.0"
	}
	return strings.TrimSpace(string(data))
}

// respondError writes an error response.
func respondError(w http.ResponseWriter, status int, msg string) {
	respondJSON(w, status, map[string]string{"error": msg})
}

// respondEmbedError maps embedder error types to proper HTTP status codes.
//   - BUSY → 503 Service Unavailable (retry with backoff)
//   - UNAVAILABLE → 503 Service Unavailable (retry after delay)
//   - BAD_QUERY → 422 Unprocessable Entity (don't retry)
//   - other → 500 Internal Server Error
func respondEmbedError(w http.ResponseWriter, err error, context string) {
	if embedder.IsBusy(err) {
		slog.Warn(context, "error", err)
		w.Header().Set("Retry-After", "2")
		respondJSON(w, 503, map[string]string{
			"error": "embedding service busy, retry",
			"type":  "BUSY",
		})
		return
	}
	if embedder.IsBadQuery(err) {
		slog.Warn(context, "error", err)
		respondJSON(w, 422, map[string]string{
			"error": "query cannot be embedded",
			"type":  "BAD_QUERY",
		})
		return
	}
	if embedder.IsUnavailable(err) {
		slog.Error(context, "error", err)
		w.Header().Set("Retry-After", "5")
		respondJSON(w, 503, map[string]string{
			"error": "embedding service unavailable",
			"type":  "UNAVAILABLE",
		})
		return
	}
	// Unknown error type
	slog.Error(context, "error", err)
	respondError(w, 500, "embedding failed")
}

// bindJSON decodes JSON from request body.
func bindJSON(r *http.Request, dst any) error {
	decoder := json.NewDecoder(http.MaxBytesReader(nil, r.Body, 1<<20)) // 1MB limit
	decoder.DisallowUnknownFields()
	return decoder.Decode(dst)
}
