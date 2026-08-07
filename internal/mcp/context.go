package mcp

// Client context: how the MCP server learns which project/workspace the
// calling agent (Hermes) is working in, so memories land in the right
// namespace automatically instead of defaulting to "global".
//
// Resolution priority for namespace/workspace on every tool call:
//   1. explicit argument passed by the caller
//   2. HTTP header (X-Mindbank-Namespace / X-Mindbank-Workspace)
//   3. pinned context (set_context tool, persisted across restarts)
//   4. derived context (active Hermes session transcript, auto-detected)
//   5. fallback ("hermes" workspace / "global" namespace)

import (
	"context"
	"encoding/json"
	"log/slog"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"sync"
	"time"

	"mindbank/internal/autocapture"
)

// clientCtx carries per-request context from the HTTP transport headers.
type clientCtx struct {
	workspace string
	namespace string
	session   string
}

type ctxKeyType struct{}

var ctxKey ctxKeyType

// withClientCtx attaches transport-level context (HTTP headers) to a request.
func withClientCtx(ctx context.Context, ws, ns, session string) context.Context {
	return context.WithValue(ctx, ctxKey, &clientCtx{workspace: ws, namespace: ns, session: session})
}

func clientCtxFrom(ctx context.Context) *clientCtx {
	if cc, ok := ctx.Value(ctxKey).(*clientCtx); ok {
		return cc
	}
	return nil
}

// pinnedContext is the set_context override, persisted so it survives restarts.
type pinnedContext struct {
	Workspace string `json:"workspace"`
	Namespace string `json:"namespace"`
	UpdatedAt string `json:"updated_at"`
}

const pinnedContextPath = ".mindbank/mcp-context.json"

func pinnedContextFile() string {
	if home, err := os.UserHomeDir(); err == nil {
		return filepath.Join(home, pinnedContextPath)
	}
	return "/tmp/mcp-context.json"
}

func loadPinnedContext() *pinnedContext {
	pc := &pinnedContext{}
	data, err := os.ReadFile(pinnedContextFile())
	if err != nil {
		return pc
	}
	if err := json.Unmarshal(data, pc); err != nil {
		slog.Warn("mcp pinned context unreadable", "error", err)
		return &pinnedContext{}
	}
	return pc
}

func savePinnedContext(pc *pinnedContext) error {
	dir := filepath.Dir(pinnedContextFile())
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return err
	}
	data, _ := json.MarshalIndent(pc, "", "  ")
	return os.WriteFile(pinnedContextFile(), data, 0o644)
}

// derivedContext caches the auto-detected namespace from the active Hermes
// transcript. The dumps are large, so we only re-parse when a newer file
// appears or the cache is stale.
type derivedContext struct {
	mu        sync.Mutex
	namespace string
	workspace string
	file      string
	fileMtime time.Time
	expires   time.Time
}

var derived = &derivedContext{}

// hermesTranscriptDirs returns the directories Hermes writes live session
// request-dumps to (the active conversation transcript).
func hermesTranscriptDirs() []string {
	home, _ := os.UserHomeDir()
	dirs := []string{filepath.Join(home, ".hermes", "sessions")}
	profiles, _ := filepath.Glob(filepath.Join(home, ".hermes", "profiles", "*", "sessions"))
	dirs = append(dirs, profiles...)
	return dirs
}

// newestTranscript finds the most recently modified Hermes request dump.
func newestTranscript() (path string, mtime time.Time) {
	var bestPath string
	var bestTime time.Time
	for _, dir := range hermesTranscriptDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "request_dump_") || !strings.HasSuffix(e.Name(), ".json") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				bestPath = filepath.Join(dir, e.Name())
			}
		}
	}
	return bestPath, bestTime
}

// deriveFromTranscript extracts message text from a Hermes request dump and
// frequency-scores the project across messages, weighting later messages a
// little more so a mid-session project switch takes over quickly. Delegates
// path parsing to autocapture.NamespaceFromText (same logic the session
// miner uses).
func deriveFromTranscript(path string) (namespace, workspace string) {
	data, err := os.ReadFile(path)
	if err != nil || len(data) > 50*1024*1024 {
		return "", ""
	}
	var dump struct {
		Request struct {
			Body struct {
				Messages []json.RawMessage `json:"messages"`
			} `json:"body"`
		} `json:"request"`
	}
	if err := json.Unmarshal(data, &dump); err != nil {
		return "", ""
	}
	var texts []string
	for _, raw := range dump.Request.Body.Messages {
		var m struct {
			Role    string `json:"role"`
			Content any    `json:"content"`
		}
		if err := json.Unmarshal(raw, &m); err != nil {
			continue
		}
		switch c := m.Content.(type) {
		case string:
			texts = append(texts, c)
		case []any:
			for _, part := range c {
				if pm, ok := part.(map[string]any); ok {
					if t, ok := pm["text"].(string); ok {
						texts = append(texts, t)
					}
				}
			}
		}
	}
	if len(texts) == 0 {
		return "", ""
	}
	counts := map[string]int{}
	n := float64(len(texts))
	for i, t := range texts {
		p := autocapture.NamespaceFromText(t)
		if p == "" || p == "global" {
			continue
		}
		weight := 10 + int(float64(i)/n*10) // later messages weigh more
		counts[p] += weight
	}
	var best string
	bestScore := 0
	for proj, score := range counts {
		if score > bestScore {
			best, bestScore = proj, score
		}
	}
	if best == "" {
		return "", ""
	}
	return best, ""
}

// activeSessionCount counts distinct session ids across Hermes request dumps.
// Hermes can run hundreds of concurrent sessions, so "newest transcript" is
// only a safe namespace signal when exactly ONE session is active.
func activeSessionCount() int {
	seen := map[string]bool{}
	for _, dir := range hermesTranscriptDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "request_dump_") {
				continue
			}
			if sid := sessionIDFromFilename(e.Name()); sid != "" {
				seen[sid] = true
			}
		}
	}
	return len(seen)
}

// sessionIDFromFilename extracts the session id from request_dump_<sid>_<ts>.json.
func sessionIDFromFilename(name string) string {
	// timestamp suffix is %Y%m%d_%H%M%S_%f (8_6_6 digits)
	i := strings.LastIndex(name, "_")
	if i < 0 {
		return ""
	}
	rest := name[:i]
	j := strings.LastIndex(rest, "_")
	if j < 0 {
		return ""
	}
	return rest[:j]
}

// sessionNamespace resolves a specific Hermes session id to a project
// namespace by parsing that session's request dumps (cached 5 min). Returns
// "" when the session has no transcripts or no project signal.
func sessionNamespace(sid string) string {
	if sid == "" {
		return ""
	}
	cacheKey := "sid:" + sid
	sessionCache.mu.Lock()
	defer sessionCache.mu.Unlock()
	if e, ok := sessionCache.entries[cacheKey]; ok && time.Since(e.setAt) < 5*time.Minute {
		return e.ns
	}
	// Newest dump for this session id across all transcript dirs.
	var bestPath string
	var bestTime time.Time
	for _, dir := range hermesTranscriptDirs() {
		entries, err := os.ReadDir(dir)
		if err != nil {
			continue
		}
		for _, e := range entries {
			if e.IsDir() || !strings.HasPrefix(e.Name(), "request_dump_"+sid+"_") {
				continue
			}
			info, err := e.Info()
			if err != nil {
				continue
			}
			if info.ModTime().After(bestTime) {
				bestTime = info.ModTime()
				bestPath = dir + "/" + e.Name()
			}
		}
	}
	ns := ""
	// state.db holds the FULL session transcript — the authoritative signal.
	// Request dumps are error snapshots of a single request and can skew
	// toward whatever that request touched, so prefer state.db when available.
	ns = sessionNamespaceFromStateDB(sid)
	if ns == "" && bestPath != "" {
		ns, _ = deriveFromTranscript(bestPath)
	}
	sessionCache.entries[cacheKey] = sessionCacheEntry{ns: ns, setAt: time.Now()}
	return ns
}

// sessionNamespaceFromStateDB resolves a session id against Hermes' state.db
// using the python3 stdlib sqlite3 helper (no Go sqlite dependency). Best
// effort: returns "" when python3 or the helper is unavailable.
func sessionNamespaceFromStateDB(sid string) string {
	home, err := os.UserHomeDir()
	if err != nil {
		return ""
	}
	helper := filepath.Join(home, "mindbank", "scripts", "hermes-session-ns.py")
	if _, err := os.Stat(helper); err != nil {
		return ""
	}
	ctx, cancel := context.WithTimeout(context.Background(), 10*time.Second)
	defer cancel()
	cmd := exec.CommandContext(ctx, "python3", helper, sid)
	out, err := cmd.Output()
	if err != nil {
		return ""
	}
	ns := strings.TrimSpace(string(out))
	if ns == "" || ns == "global" {
		return ""
	}
	return ns
}

type sessionCacheEntry struct {
	ns    string
	setAt time.Time
}

var sessionCache = struct {
	mu      sync.Mutex
	entries map[string]sessionCacheEntry
}{entries: map[string]sessionCacheEntry{}}

// deriveNamespace returns the cached auto-detected namespace from the newest
// Hermes transcript — but ONLY when the transcript signal is unambiguous
// (exactly one active session). With concurrent sessions the newest dump is
// one of many and guessing would mis-tag memories, so it is disabled and
// "global" is returned (explicit args / headers / set_context still apply).
func (s *Server) deriveNamespace() string {
	derived.mu.Lock()
	defer derived.mu.Unlock()
	now := time.Now()
	if derived.namespace != "" && now.Before(derived.expires) {
		return derived.namespace
	}
	ns := ""
	if n := activeSessionCount(); n == 1 {
		path, mtime := newestTranscript()
		ns, _ = deriveFromTranscript(path)
		derived.file, derived.fileMtime = path, mtime
	} else if n > 1 {
		derived.file = "ambiguous" // marker: >1 concurrent sessions
	}
	if ns == "" {
		ns = "global"
	}
	derived.namespace, derived.expires = ns, now.Add(2*time.Minute)
	return ns
}

// resolveContext computes the effective workspace + namespace for a tool call.
func (s *Server) resolveContext(ctx context.Context, argWorkspace, argNamespace string) (string, string) {
	ws := argWorkspace
	ns := argNamespace
	if cc := clientCtxFrom(ctx); cc != nil {
		if ws == "" {
			ws = cc.workspace
		}
		if ns == "" {
			ns = cc.namespace
		}
	}
	if ws == "" {
		ws = s.pinned.Workspace
	}
	if ns == "" {
		ns = s.pinned.Namespace
	}
	if ns == "" {
		if cc := clientCtxFrom(ctx); cc != nil && cc.session != "" {
			ns = sessionNamespace(cc.session) // per-session transcript, accurate under concurrency
		}
	}
	if ns == "" {
		ns = s.deriveNamespace() // only safe with a single active session
	}
	if ws == "" {
		ws = "hermes"
	}
	if ns == "" {
		ns = "global"
	}
	return ws, ns
}
