# MindBank Release Notes

## 0.6.1 (2026-08)

Public release line. This version ships the clean release branch
(`release/0.6.1`), the one-command installer, and the full JSPACE/MCP feature set.

> Existing 0.6.0 installs see this as an update (the updater compares version strings,
> so the 0.6.0 tag alone would not have triggered it).

## 0.6.0 (2026-08)

### Global workspace (JSPACE)
- New **JSPACE** dashboard tab: live fleet reasoning feed, capacity-calibrated workspace heatmap with concept families and leases, workspace event stream, specialists panel, ingestion funnel, and provenance labels (RAW session data vs CURATED memory).
- **Workspace feedback loop** (toggleable): workspace-active memories are protected from TTL expiry and boosted in recall.
- **Workspace event stream** (`workspace_events`): every created/reinforced/entered/left/consumed/expired transition is captured.
- **Namespace auto-attribution**: per-session project detection from Hermes transcripts (`state.db`) and per-request `X-Mindbank-Session`/`X-Mindbank-Namespace` headers — correct even with hundreds of concurrent sessions.

### MCP server
- **22 tools**, including new `get_node`, `delete_node`, `create_nodes` (batch), `recent`, `history`, `set_context`, `get_context`.
- FTS-only fallback when the embedder is unavailable; `node_types` filters; dedup-aware create reporting.
- Hermes MCP client patch sends the session id per call for accurate memory attribution.

### Memory & search
- Edge writes now invalidate cached search results (fixes stale graph-expansion recall).
- Search/lease metadata casts hardened (no `::boolean` on user data).
- Orphan dedup-hash cleanup; orphan-edge repair tool.

### Reliability
- Single-command installer (`scripts/install.sh`), clean release build (`scripts/build-release.sh`).
- Workspace event retention (30 days); lease snapshot excludes soft-deleted nodes.

## 0.5.2 (2026-06)
- Dream engine (neural consolidation), taxonomy, advanced features, auto-forgetting profiles.
- Session mining with LLM extraction; MCP HTTP transport (Hermes integration).

## 0.5.1 (2026-05)
- Brain3D visualization, namespace clustering, security visualization.
