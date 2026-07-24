# MindBank v0.6.0

A major update focused on making agent **recall actually work end-to-end**, a fully
reworked dashboard, and a customizable LLM backend.

## Highlights

### Recall & memory quality (critical fixes)
- **Fixed silent search failure:** the Hermes plugin checked `isinstance(result, list)`
  but `/search/hybrid` returns `{nodes: [...]}`, so `mindbank_search` and automatic
  prefetch always returned "No memories found". Recall now works.
- **Namespace detection fixed:** the plugin used `re` without importing it, so every
  project silently fell back to the `hermes` namespace. Now resolves real project names.
- **Generic namespaces recall globally:** `hermes`/`global`/`default` sessions now draw
  from the whole graph; real project namespaces stay isolated (with broaden-on-empty).
- **Session-shell nodes excluded** from search, snapshot, neighbors, and dependence
  expansion — recall returns distilled knowledge, not transcript filenames.
- Wake-up snapshot is regenerated fresh for the global view instead of serving a stale blob.

### New: Settings tab (LLM backend)
- Configure any OpenAI-compatible LLM (OpenRouter, OpenAI, Groq, DeepSeek, local Ollama)
  from the dashboard — no GPU required. Provider auto-detect, key masking, Test Connection.

### Dashboard / Brain_3D overhaul
- Unified visual theme, force-directed 3D graph, clearer node/edge interaction,
  working top-row controls, and a fixed Debug/heal panel.

### MCP server
- All 15 tools verified functional. `mine_sessions` and `conflicts detect` now trigger
  real work in the background instead of returning stubs. Edge weight defaults fixed
  (omitted weight → 1.0, not 0.0). Per-request timeout added to the HTTP transport.

## Updating
Existing installs: `bash scripts/update.sh`
New installs: see `QUICKSTART.md`.
