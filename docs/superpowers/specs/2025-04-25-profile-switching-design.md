# MindBank Profile Switching — Design Spec

## Goal
Enable users to switch between Hermes profiles (e.g., "default", "work", "personal") in the MindBank dashboard, with each profile's data properly isolated via workspace filtering across the entire stack: backend API, frontend UI, MCP server, and Hermes plugin.

## Architecture Overview

```
┌─────────────────────────────────────────────────────────────────────────────┐
│                              HERMES AGENT                                    │
│  ┌──────────────┐  ┌──────────────┐  ┌──────────────┐                       │
│  │   Profile A  │  │   Profile B  │  │   Profile C  │  ← hermes profiles    │
│  │  (workspace) │  │  (workspace) │  │  (workspace) │                       │
│  └──────┬───────┘  └──────┬───────┘  └──────┬───────┘                       │
│         │                 │                 │                                │
│         └─────────────────┴─────────────────┘                                │
│                           │                                                  │
│                    ┌──────┴──────┐                                           │
│                    │  Plugin     │  mindbank/__init__.py                      │
│                    │  (namespace │  ── sends workspace to API                 │
│                    │   mapping)  │                                            │
│                    └──────┬──────┘                                           │
│                           │ HTTP /api/v1                                     │
└───────────────────────────┼─────────────────────────────────────────────────┘
                            │
┌───────────────────────────┼─────────────────────────────────────────────────┐
│                           ▼                                                  │
│                    ┌──────────────┐                                          │
│  MIND BANK         │   API Server │  localhost:8095                          │
│  BACKEND           │   (Go)       │  ── filters ALL queries by workspace     │
│                    └──────┬───────┘                                          │
│                           │                                                  │
│                    ┌──────┴──────┐                                           │
│                    │  PostgreSQL │  ── workspace_name column isolates data  │
│                    │  + pgvector │                                            │
│                    └─────────────┘                                            │
│                                                                              │
│  ┌────────────────────────────────────────────────────────────────────────┐  │
│  │  MCP Server (stdio)  ── optional bridge, also respects workspace       │  │
│  └────────────────────────────────────────────────────────────────────────┘  │
└─────────────────────────────────────────────────────────────────────────────┘
                            │
                            │ HTTP /api/v1 (same server)
                            ▼
                    ┌──────────────┐
                    │  Dashboard   │  graph.html / index.html
                    │  (Frontend)  │  ── profile switcher in UI
                    └──────────────┘
```

## Core Principle
**Profile = Workspace.** Hermes profiles (e.g., "default", "work") map 1:1 to MindBank `workspace_name`. When a user switches profiles in Hermes, the plugin sends the new workspace name to MindBank. The dashboard allows manual profile switching via a dropdown that sets the workspace filter.

## Data Model (Existing — No Schema Changes)

The `workspace_name` column already exists on all tables:
- `nodes.workspace_name` — DEFAULT 'default'
- `edges.workspace_name` — DEFAULT 'default'
- `sessions.workspace_name` — DEFAULT 'hermes'
- `snapshots.workspace_name` — DEFAULT 'default'

The `namespace` column also exists for project-level isolation within a workspace.

## Component Designs

### 1. Backend API (Go)

**Current State:** Most endpoints already accept `?workspace=` query parameter and filter by it. The graph endpoint (`/graph`) already filters by workspace.

**Required Fixes:**
- `ask.go` — `/ask` POST endpoint: read `workspace` from JSON body, pass to search repo
- `node.go` — `/nodes` GET endpoint: ensure workspace filter is applied when listing nodes
- `session.go` — `/sessions/:id` GET endpoint: ensure workspace filter is applied
- All endpoints must default to `"default"` workspace if not specified

**New Endpoint:**
- `GET /workspaces` — list all distinct workspace names from nodes table (for populating the profile switcher)

### 2. Frontend Dashboard (graph.html)

**Current State:** No profile/workspace awareness. All data shown is unfiltered (defaults to 'default' workspace).

**Required Changes:**
- Add profile switcher dropdown in the top nav bar
- Store selected profile in `localStorage` (key: `mindbank_profile`)
- On profile change, reload all data with `?workspace=<profile>`
- Display current profile name in the UI
- Fetch available profiles from `GET /workspaces` on load

**UI Placement:** Next to the "Sync Sessions" button in the top bar.

### 3. Frontend Index (index.html)

**Current State:** The main landing page has no profile awareness.

**Required Changes:**
- Read `mindbank_profile` from localStorage
- Pass workspace to any API calls made from index.html
- Show current profile in header

### 4. Observer Tab (observer-tab.js)

**Current State:** Has `profile` concept but maps it to `namespace` instead of `workspace`.

**Required Changes:**
- Map `profile` → `workspace_name` (not namespace)
- Send `workspace` parameter in all fetch calls
- Keep `namespace` for project-level isolation within the workspace

### 5. Hermes Plugin (plugins/memory/mindbank/__init__.py)

**Current State:** Uses `namespace` for isolation. Has no `workspace` concept. The `_ensure_session()` method hardcodes `workspace_name: "hermes"`.

**Required Changes:**
- Add `workspace` config field (default: "default")
- Map Hermes profile name → MindBank workspace name
- Send `workspace` in ALL API calls (store, search, ask, snapshot, etc.)
- Update `_ensure_session()` to use configured workspace
- Update `system_prompt_block()` to filter by workspace + namespace
- Update `sync_turn()` and `on_session_end()` to include workspace
- Update all tool schemas to include optional `workspace` parameter

**Profile Detection:**
```python
# Priority:
# 1. Explicit config: mindbank.json "workspace" field
# 2. Environment: MINDBANK_WORKSPACE
# 3. Hermes profile: kwargs.get("profile", "default")
# 4. Fallback: "default"
```

### 6. MCP Server (internal/mcp/server.go)

**Current State:** Already accepts `workspace` parameter in most tool calls. Defaults to "hermes" in `toolSnapshot`.

**Required Changes:**
- Fix `toolSnapshot` default workspace from "hermes" to "default"
- Ensure all tool handlers pass workspace through to repos
- Add workspace parameter to `toolNeighbors` if not already present

### 7. Session Miner (scripts/session_miner.py)

**Current State:** Creates sessions with `workspace_name: "hermes"`. No profile/workspace switching support.

**Required Changes:**
- Accept `--workspace` CLI argument (default: "hermes")
- Pass workspace to session creation and node creation
- Store workspace in session metadata

## API Contract

### New Endpoint
```
GET /api/v1/workspaces
Response: ["default", "hermes", "work", "personal"]
```

### Updated Endpoints (all now accept workspace)
All existing endpoints that accept `?workspace=` or `{"workspace": "..."}` continue to work. Default workspace is `"default"`.

## Error Handling
- Invalid workspace: return 400 Bad Request
- Workspace not found: return empty results (not error)
- Missing workspace param: default to "default"

## Security
- No auth required (local-only API)
- Workspace is purely a filter — no access control
- All data is visible to all workspaces (isolation is organizational, not security)

## Testing Plan
1. Create nodes in workspace "test-a", verify they don't appear in "test-b"
2. Switch profiles in dashboard, verify data reloads
3. Run session miner with --workspace=test, verify sessions created in correct workspace
4. Test Hermes plugin with different profiles, verify isolation
5. Test MCP tools with workspace parameter

## Migration
- No database migration needed (workspace_name column exists)
- Existing data with workspace_name='default' or 'hermes' remains accessible
- Users can manually re-tag data if needed via API
