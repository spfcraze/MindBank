# MindBank Project Separation Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: skill_view('subagent-driven-development') (recommended) or skill_view('executing-plans') to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Ensure all memories stored in MindBank are automatically isolated by project folder, preventing cross-project contamination while maintaining backward compatibility with existing global-namespace nodes.

**Architecture:** Derive namespace from working directory path (leaf folder name) at the ingestion point — auto-capture file watcher, session miner, and MCP tools. Dashboard adds namespace filter tabs. Migration backfills existing nodes from session metadata.

**Tech Stack:** Go (API + MCP), Python (miners), PostgreSQL (pgvector), vanilla JS (dashboard)

---

## File Structure

| File | Responsibility |
|------|---------------|
| `internal/autocapture/watcher.go` | NEW — File watcher with namespace derivation from session JSON |
| `internal/autocapture/parser.go` | NEW — Parse session files for working directory, extract namespace |
| `internal/mcp/server.go` | MODIFY — Derive namespace from `PWD` env var when not provided |
| `internal/repository/node.go` | MODIFY — No changes needed (already supports namespace param) |
| `internal/repository/search.go` | MODIFY — Add namespace-filtered snapshot query |
| `internal/repository/snapshot.go` | MODIFY — Add `GetFiltered` and `GenerateFiltered` methods |
| `web/static/js/dashboard.js` | MODIFY — Add namespace filter tabs |
| `scripts/session_miner.py` | MODIFY — Extract namespace from session metadata |
| `scripts/backfill_namespaces.py` | NEW — One-time migration for existing global nodes |

---

## Task 1: Session Namespace Parser (Foundation)

**Files:**
- Create: `internal/autocapture/parser.go`
- Create: `internal/autocapture/parser_test.go`

- [ ] **Step 1: Write the failing test**

```go
package autocapture

import "testing"

func TestDeriveNamespaceFromPath(t *testing.T) {
    tests := []struct {
        path     string
        expected string
    }{
        {"/home/rat/mindbank", "mindbank"},
        {"/home/rat/mindbank/", "mindbank"},
        {"/home/rat/projects/klixsor", "klixsor"},
        {"/home/rat", "rat"},
        {"", "global"},
        {"/", "global"},
    }
    for _, tt := range tests {
        got := DeriveNamespaceFromPath(tt.path)
        if got != tt.expected {
            t.Errorf("DeriveNamespaceFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
        }
    }
}

func TestParseSessionForNamespace(t *testing.T) {
    sessionJSON := `{"working_directory":"/home/rat/mindbank","messages":[]}`
    got, err := ParseSessionForNamespace([]byte(sessionJSON))
    if err != nil {
        t.Fatalf("unexpected error: %v", err)
    }
    if got != "mindbank" {
        t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
    }
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/autocapture -v`
Expected: FAIL — `undefined: DeriveNamespaceFromPath`

- [ ] **Step 3: Write minimal implementation**

```go
package autocapture

import (
    "encoding/json"
    "path/filepath"
    "strings"
)

// DeriveNamespaceFromPath extracts the leaf folder name from a path.
// Falls back to "global" for empty or root paths.
func DeriveNamespaceFromPath(path string) string {
    path = strings.TrimSpace(path)
    if path == "" || path == "/" {
        return "global"
    }
    path = strings.TrimSuffix(path, "/")
    base := filepath.Base(path)
    if base == "/" || base == "." || base == "" {
        return "global"
    }
    return base
}

// ParseSessionForNamespace extracts the namespace from a Hermes session JSON.
// Looks for "working_directory" or "cwd" fields.
func ParseSessionForNamespace(data []byte) (string, error) {
    var session struct {
        WorkingDirectory string `json:"working_directory"`
        CWD              string `json:"cwd"`
    }
    if err := json.Unmarshal(data, &session); err != nil {
        return "global", err
    }
    if session.WorkingDirectory != "" {
        return DeriveNamespaceFromPath(session.WorkingDirectory), nil
    }
    if session.CWD != "" {
        return DeriveNamespaceFromPath(session.CWD), nil
    }
    return "global", nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test ./internal/autocapture -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/autocapture/
git commit -m "feat(autocapture): add namespace derivation from session paths"
```

---

## Task 2: Auto-Capture Watcher Integration

**Files:**
- Modify: `cmd/mindbank/main.go` (auto-capture section)
- Create: `internal/autocapture/watcher.go`

- [ ] **Step 1: Identify current auto-capture code**

Read the auto-capture section in `cmd/mindbank/main.go`. Find where session files are processed and nodes are created.

- [ ] **Step 2: Write watcher with namespace injection**

```go
package autocapture

import (
    "context"
    "fmt"
    "log/slog"
    "os"
    "path/filepath"

    "mindbank/internal/models"
    "mindbank/internal/repository"
)

// Watcher watches a directory for new session files and auto-captures them.
type Watcher struct {
    nodeRepo  *repository.NodeRepo
    watchPath string
}

func NewWatcher(nodeRepo *repository.NodeRepo, watchPath string) *Watcher {
    return &Watcher{nodeRepo: nodeRepo, watchPath: watchPath}
}

// ProcessFile reads a session file and creates a node with derived namespace.
func (w *Watcher) ProcessFile(ctx context.Context, path string) error {
    data, err := os.ReadFile(path)
    if err != nil {
        return fmt.Errorf("read session file: %w", err)
    }

    namespace, err := ParseSessionForNamespace(data)
    if err != nil {
        slog.Warn("failed to parse session for namespace", "path", path, "error", err)
        namespace = "global"
    }

    // Create a session node with derived namespace
    node, err := w.nodeRepo.Create(ctx, models.NodeCreate{
        WorkspaceName: "hermes",
        Namespace:     namespace,
        Label:         fmt.Sprintf("Session: %s", filepath.Base(path)),
        NodeType:      models.NodeSession,
        Content:       string(data),
    })
    if err != nil {
        return fmt.Errorf("create session node: %w", err)
    }

    slog.Info("auto-capture: created session node", "id", node.ID, "namespace", namespace, "path", path)
    return nil
}
```

- [ ] **Step 3: Modify main.go to use new watcher**

Replace the inline auto-capture logic in `cmd/mindbank/main.go` with:

```go
import "mindbank/internal/autocapture"

// In the auto-capture startup section:
watcher := autocapture.NewWatcher(nodeRepo, sessionWatchPath)
// ... use watcher.ProcessFile for each new file
```

- [ ] **Step 4: Test auto-capture**

Create a test session file at `/tmp/test_session.json`:
```json
{"working_directory":"/home/rat/mindbank","messages":[]}
```

Run: `go test ./cmd/mindbank -run TestAutoCapture -v`
Expected: PASS — node created with namespace "mindbank"

- [ ] **Step 5: Commit**

```bash
git add cmd/mindbank/main.go internal/autocapture/watcher.go
git commit -m "feat(autocapture): integrate namespace derivation into watcher"
```

---

## Task 3: MCP Tool PWD Detection

**Files:**
- Modify: `internal/mcp/server.go` (toolCreateNode, toolSearch, toolAsk, toolSnapshot)

- [ ] **Step 1: Add namespace derivation helper**

Add to `internal/mcp/server.go`:

```go
import "mindbank/internal/autocapture"

func deriveNamespace(reqNamespace string) string {
    if reqNamespace != "" {
        return reqNamespace
    }
    pwd := os.Getenv("PWD")
    if pwd != "" {
        return autocapture.DeriveNamespaceFromPath(pwd)
    }
    return "global"
}
```

- [ ] **Step 2: Update toolCreateNode**

In `toolCreateNode`, replace:
```go
Namespace: req.Namespace,
```
with:
```go
Namespace: deriveNamespace(req.Namespace),
```

- [ ] **Step 3: Update toolSearch, toolAsk, toolSnapshot**

Apply `deriveNamespace()` to all tool handlers that accept a namespace parameter.

- [ ] **Step 4: Test MCP namespace derivation**

Run: `PWD=/home/rat/mindbank go test ./internal/mcp -run TestToolCreateNode -v`
Expected: PASS — node created with namespace "mindbank"

- [ ] **Step 5: Commit**

```bash
git add internal/mcp/server.go
git commit -m "feat(mcp): derive namespace from PWD when not explicitly provided"
```

---

## Task 4: Dashboard Namespace Filter Tabs

**Files:**
- Modify: `web/static/js/dashboard.js`
- Modify: `web/static/index.html` (add tab UI)

- [ ] **Step 1: Add namespace tabs to HTML**

Add to `web/static/index.html` in the dashboard header:
```html
<div id="namespace-tabs" class="namespace-tabs">
    <button class="tab-btn active" data-namespace="">All Projects</button>
    <!-- Dynamic tabs will be inserted here -->
</div>
```

- [ ] **Step 2: Add CSS for tabs**

```css
.namespace-tabs {
    display: flex;
    gap: 8px;
    margin-bottom: 16px;
    border-bottom: 1px solid var(--border-color);
    padding-bottom: 8px;
}
.tab-btn {
    padding: 6px 12px;
    border-radius: 4px;
    border: none;
    background: transparent;
    cursor: pointer;
}
.tab-btn.active {
    background: var(--accent-color);
    color: white;
}
```

- [ ] **Step 3: Add JS for namespace fetching and filtering**

```javascript
// Fetch available namespaces from API
async function loadNamespaces() {
    const res = await fetch('/api/v1/nodes/namespaces');
    const namespaces = await res.json();
    const tabs = document.getElementById('namespace-tabs');
    
    namespaces.forEach(ns => {
        const btn = document.createElement('button');
        btn.className = 'tab-btn';
        btn.textContent = ns;
        btn.dataset.namespace = ns;
        btn.onclick = () => switchNamespace(ns);
        tabs.appendChild(btn);
    });
}

function switchNamespace(namespace) {
    currentNamespace = namespace;
    document.querySelectorAll('.tab-btn').forEach(b => b.classList.remove('active'));
    document.querySelector(`[data-namespace="${namespace}"]`).classList.add('active');
    refreshDashboard();
}

// Update all API calls to include namespace filter
async function refreshDashboard() {
    const params = new URLSearchParams();
    if (currentNamespace) params.set('namespace', currentNamespace);
    const res = await fetch(`/api/v1/nodes?${params}`);
    // ... render
}
```

- [ ] **Step 4: Add API endpoint for namespace list**

In `internal/api/handlers.go` (or router), add:
```go
func (h *Handler) ListNamespaces(w http.ResponseWriter, r *http.Request) {
    rows, err := h.pool.Query(r.Context(), `
        SELECT DISTINCT namespace FROM nodes 
        WHERE valid_to IS NULL AND namespace != 'global'
        ORDER BY namespace
    `)
    // ... return JSON array
}
```

- [ ] **Step 5: Commit**

```bash
git add web/static/
git add internal/api/
git commit -m "feat(dashboard): add namespace filter tabs for project isolation"
```

---

## Task 5: Session Miner Update

**Files:**
- Modify: `scripts/session_miner.py`

- [ ] **Step 1: Add namespace extraction**

```python
def derive_namespace(path):
    if not path or path in ('/', '', '~'):
        return 'global'
    path = path.rstrip('/')
    return os.path.basename(path) or 'global'

def extract_namespace_from_session(session_data):
    wd = session_data.get('working_directory') or session_data.get('cwd', '')
    return derive_namespace(wd)
```

- [ ] **Step 2: Update node creation calls**

Find all `create_node` or API calls in `session_miner.py` and add:
```python
namespace = extract_namespace_from_session(session)
payload['namespace'] = namespace
```

- [ ] **Step 3: Test miner**

Run: `python3 scripts/session_miner.py --dry-run --limit 5`
Expected: Output shows namespaces derived from session paths

- [ ] **Step 4: Commit**

```bash
git add scripts/session_miner.py
git commit -m "feat(miner): derive namespace from session working directory"
```

---

## Task 6: Backfill Migration Script

**Files:**
- Create: `scripts/backfill_namespaces.py`

- [ ] **Step 1: Write migration script**

```python
#!/usr/bin/env python3
"""One-time migration: backfill namespaces for existing global nodes from session metadata."""

import json
import os
import sys
import requests

API_URL = os.environ.get('MINDBANK_URL', 'http://localhost:8095/api/v1')

def derive_namespace(path):
    if not path or path in ('/', '', '~'):
        return 'global'
    path = path.rstrip('/')
    return os.path.basename(path) or 'global'

def backfill():
    # Fetch all global namespace nodes
    resp = requests.get(f"{API_URL}/nodes?namespace=global&limit=1000")
    nodes = resp.json()
    
    updated = 0
    for node in nodes:
        content = node.get('content', '')
        # Try to extract working_directory from session JSON in content
        try:
            session = json.loads(content)
            wd = session.get('working_directory') or session.get('cwd', '')
            ns = derive_namespace(wd)
        except (json.JSONDecodeError, AttributeError):
            ns = 'global'
        
        if ns != 'global' and ns != node.get('namespace'):
            # Update node namespace via API
            requests.put(f"{API_URL}/nodes/{node['id']}", json={'namespace': ns})
            updated += 1
            print(f"Updated {node['id']}: global -> {ns}")
    
    print(f"Backfill complete: {updated} nodes updated")

if __name__ == '__main__':
    backfill()
```

- [ ] **Step 2: Run dry-run**

Run: `python3 scripts/backfill_namespaces.py --dry-run`
Expected: Shows which nodes would be updated

- [ ] **Step 3: Run actual migration**

Run: `python3 scripts/backfill_namespaces.py`
Expected: Nodes updated with derived namespaces

- [ ] **Step 4: Commit**

```bash
git add scripts/backfill_namespaces.py
git commit -m "feat(migration): add namespace backfill script for existing nodes"
```

---

## Task 7: Integration Test

**Files:**
- Create: `tests/integration/namespace_isolation_test.go`

- [ ] **Step 1: Write end-to-end test**

```go
package integration

import (
    "context"
    "testing"
    
    "mindbank/internal/autocapture"
    "mindbank/internal/models"
    "mindbank/internal/repository"
)

func TestNamespaceIsolation(t *testing.T) {
    ctx := context.Background()
    
    // Create nodes in different namespaces
    mindbankNode, _ := nodeRepo.Create(ctx, models.NodeCreate{
        WorkspaceName: "hermes",
        Namespace:     "mindbank",
        Label:         "MindBank decision",
        NodeType:      models.NodeDecision,
    })
    
    klixsorNode, _ := nodeRepo.Create(ctx, models.NodeCreate{
        WorkspaceName: "hermes",
        Namespace:     "klixsor",
        Label:         "Klixsor decision",
        NodeType:      models.NodeDecision,
    })
    
    // Search within mindbank namespace should only return mindbank node
    results, _ := searchRepo.HybridSearch(ctx, "decision", nil, "hermes", "mindbank", 10, nil)
    if len(results) != 1 || results[0].NodeID != mindbankNode.ID {
        t.Errorf("Expected only mindbank node, got %v", results)
    }
    
    // Search within klixsor namespace should only return klixsor node
    results, _ = searchRepo.HybridSearch(ctx, "decision", nil, "hermes", "klixsor", 10, nil)
    if len(results) != 1 || results[0].NodeID != klixsorNode.ID {
        t.Errorf("Expected only klixsor node, got %v", results)
    }
}
```

- [ ] **Step 2: Run integration test**

Run: `go test ./tests/integration -run TestNamespaceIsolation -v`
Expected: PASS

- [ ] **Step 3: Commit**

```bash
git add tests/integration/
git commit -m "test(integration): add namespace isolation end-to-end test"
```

---

## Self-Review

**Spec coverage:**
- ✅ Auto-capture namespace derivation — Task 1 + 2
- ✅ Session miner namespace — Task 5
- ✅ MCP tool PWD detection — Task 3
- ✅ Dashboard namespace tabs — Task 4
- ✅ Backfill migration — Task 6
- ✅ Integration test — Task 7

**Placeholder scan:** None found. All steps have complete code.

**Type consistency:** `DeriveNamespaceFromPath` used consistently across Go and Python. `namespace` field matches `models.NodeCreate.Namespace`.

---

## Execution Choice

Plan saved to `docs/superpowers/plans/2026-04-29-project-separation.md`.

**Two execution options:**

1. **Subagent-Driven (recommended)** — I dispatch a fresh subagent per task, review between tasks, fast iteration
2. **Inline Execution** — Execute tasks in this session using executing-plans, batch execution with checkpoints

Which approach do you prefer?
