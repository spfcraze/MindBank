MindBank MCP Server — HTTP Transport Mode

## Overview

The MindBank MCP server supports two transport modes:

1. **stdio** (default) — For MCP clients that connect via stdin/stdout
2. **HTTP** — For MCP clients that connect via HTTP (e.g., Hermes with HTTP transport)

## Why HTTP Mode?

Hermes' native MCP stdio transport has a `ClosedResourceError` bug where the connection
drops after tool discovery. The server works fine (verified via `mcporter` CLI), but
Hermes' stdio client transport fails.

HTTP mode bypasses this entirely.

## Usage

### Start HTTP Mode

```bash
cd ~/mindbank
./mindbank-mcp --transport http --http-port 8096
```

Or set env vars:
```bash
export MCP_TRANSPORT=http
export MCP_HTTP_PORT=8096
./mindbank-mcp
```

### Configure Hermes

In `~/.hermes/config.yaml`:

```yaml
mcp_servers:
  mindbank:
    url: http://127.0.0.1:8096/mcp  # HTTP transport
    # command: ~/mindbank/mindbank-mcp  # stdio transport (broken with Hermes)
    timeout: 30
    enabled: true
```

### Test

```bash
hermes mcp test mindbank
```

## HTTP Endpoints

The HTTP server exposes these endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mcp` | POST | MCP JSON-RPC endpoint |
| `/mcp/initialize` | POST | Initialize connection |
| `/mcp/tools/list` | GET | List available tools |
| `/mcp/tools/call` | POST | Call a tool |
| `/health` | GET | Health check |

## Request Format

POST to `/mcp` with JSON-RPC 2.0 body:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "query": "Polymarket",
      "limit": 5
    }
  }
}
```

## Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "- [fact] Polymarket CLOB+Data APIs are FREE..."
      }
    ],
    "isError": false
  }
}
```

## Tools

| Tool | Description |
|------|-------------|
| `create_node` | Create a memory (dedup-aware: reports "created" vs "already existed — reinforced") |
| `create_nodes` | Batch-create up to 50 memories in one call; per-item created/reinforced/failed report |
| `update_node` | Update label/content/summary/importance/epistemic_label/status (creates a new version) |
| `delete_node` | Soft-delete a node (workspace-scoped; version history retained) |
| `get_node` | Fetch a single node by ID with full content, metadata, and connection count |
| `search` | Hybrid FTS + semantic search; `node_types` filter; FTS-only fallback when Ollama is down |
| `ask` | Natural-language recall with context snippet (token-lean) |
| `recent` | Most recently created/updated memories (recency recall) |
| `snapshot` | Pre-computed wake-up context of the most important memories |
| `neighbors` | Nodes connected to a given node (depth-1 traversal) |
| `history` | Temporal version history of a node |
| `create_edge` | Create a connection between two nodes |
| `dependence` | Trace causal precursors (domain of dependence) for a node or query |
| `refine_connectivity` | Feedback-driven topological editing (missing_context / too_much_noise / granularity_mismatch) |
| `evolution` | Evolution maturity score for a node |
| `cluster_sessions` | Group session nodes into episodic clusters by embedding similarity |
| `list_namespaces` | List namespaces with node counts |
| `conflicts` | List or detect knowledge conflicts |
| `dream_status` | Dream Engine (neural consolidation) cycle status |
| `mine_sessions` | Trigger background session mining + report session count |

## Client Context & Namespaces (auto-derivation)

MindBank MCP auto-detects which project you're working in, so memories land in
the right namespace instead of `global`.

**Resolution priority** for `workspace`/`namespace` on every call:
1. Explicit argument passed to the tool
2. HTTP header: `X-Mindbank-Workspace` / `X-Mindbank-Namespace` /
   `X-Mindbank-Session` (session id resolved to that session's project)
3. Pinned context (`set_context` tool, persisted in `~/.mindbank/mcp-context.json`)
4. Auto-derived from Hermes transcripts — **only when unambiguous**:
   - `X-Mindbank-Session: <sid>` → that session's project, resolved from Hermes'
     `state.db` full transcript (authoritative), falling back to the session's
     `request_dump_<sid>_*.json` error snapshots (accurate even with hundreds
     of concurrent sessions)
   - otherwise, the newest `request_dump_*.json` is used **only when exactly one
     Hermes session is active** — with multiple concurrent sessions the server
     does NOT guess (it returns `global`) to avoid mis-tagging memories
5. Fallback: workspace `hermes`, namespace `global`

**Concurrent sessions (recommended setup).** Hermes can run hundreds of sessions
at once; "newest transcript" is then meaningless. Make each session identify its
project:
- Add the header to `mcp_servers.mindbank` in `~/.hermes/config.yaml` (and each
  profile config):
  ```yaml
  mcp_servers:
    mindbank:
      url: http://127.0.0.1:8096/mcp
      headers:
        X-Mindbank-Namespace: ${MINDBANK_NAMESPACE}
      enabled: true
  ```
- Launch Hermes with `scripts/hermes-mind.sh` (alias `hermes=hermes-mind`):
  it sets `MINDBANK_NAMESPACE` to the launch directory's project name before
  exec'ing Hermes, so every memory from that session lands in that project.
  Unset `MINDBANK_NAMESPACE` sends an empty header, which the server ignores.
- Hermes MCP client (patched `hermes-agent/tools/mcp_tool.py`) now also sends
  `X-Mindbank-Session: <session id>` automatically on every HTTP MCP call,
  so MindBank resolves the namespace from that session's own transcript even
  when the launcher wrapper isn't used. No config needed for this path.

If auto-detection is wrong or you work outside `/home/<user>/<project>`, pin it
explicitly:

```jsonc
// tools/call set_context
{ "workspace": "hermes", "namespace": "klixsor" }
// clear: { "namespace": "" }
```

`get_context` shows the effective context (header / pinned / derived).
Clients that know their project can also send `X-Mindbank-Namespace` on every
request — that takes priority over both pinned and derived context.

## Fallback: mcporter CLI

If HTTP mode also has issues, use `mcporter` directly:

```bash
# List tools
npx mcporter list --stdio "./mindbank-mcp" --name mindbank

# Call search
npx mcporter call --stdio "./mindbank-mcp" search query="test" limit=5

# Call create_node
npx mcporter call --stdio "./mindbank-mcp" create_node label="Test" type="fact" content="..."
```

## Implementation Notes

The HTTP transport is implemented in `internal/mcp/http_server.go`.
It reuses the same tool handlers as the stdio server but wraps them
in an HTTP handler with proper JSON-RPC request/response handling.

## Stdio Mode Still Works

stdio mode is NOT removed. It still works with:
- `mcporter` CLI
- Claude Desktop
- Cursor
- Any MCP client with proper stdio transport

Only Hermes' native stdio transport is affected.
