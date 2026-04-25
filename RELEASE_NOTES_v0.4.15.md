# MindBank v0.4.15 Release Notes

## Profile Separation + Namespace Detection Fixes

### Session Miner (scripts/session_miner.py)
- **Fixed namespace detection**: Now detects project names from working directory paths (e.g., `/home/rat/mindbank` → `mindbank`) instead of falling back to the Linux username
- **Added profile tracking**: All nodes created by the miner are tagged with `metadata.profile` for true multi-profile separation
- **Profile-specific mining**: Supports `--profile` flag to mine only sessions from a specific profile folder

### Hermes Memory Plugin (plugins/mindbank/__init__.py)
- **Fixed `_detect_namespace()`**: Added working directory path pattern matching with filtering for common non-project directories
- **Added `_detect_profile()`**: Auto-detects current Hermes profile from environment or config
- **Profile metadata**: All nodes created via `sync_turn()`, `on_session_end()`, `on_memory_write()`, and `_handle_store()` now include `metadata.profile`

### Start Script (start.sh)
- **Fixed log path bug**: `MB_LOGFILE` was using undefined `MB_DIR` variable, causing "Permission denied" and server startup failure

### API Endpoints Verified
All endpoints used by the plugin are working correctly:
- GET /health, GET /snapshot
- POST /search/hybrid
- GET /analyze/gaps, GET /analyze/diff, GET /analyze/contradictions
- POST /edges/batch, POST /nodes, POST /ask
- GET /nodes/{id}/neighbors

## Files Changed
- `scripts/session_miner.py` — namespace detection, profile tracking
- `plugins/mindbank/__init__.py` — namespace detection, profile tracking
- `start.sh` — log path fix
