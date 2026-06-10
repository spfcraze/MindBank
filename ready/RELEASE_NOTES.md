# MindBank v0.5.2

## What's Fixed
- **Password redaction trap**: Fallback DSN in `internal/config/config.go` now uses correct password `mindbank` instead of redacted placeholder `***`
- **Binary rebuild**: `mindbank-mcp` rebuilt with patched fallback DSN
- **MCP auto-heal**: Added `mindbank-autoheal.sh` script for self-healing when MCP is down
- **systemd service**: `mindbank-mcp.service` for auto-start on WSL login with restart-on-failure
- **Wrapper script**: `mindbank-mcp-wrapper.sh` loads `.env` correctly before starting MCP

## What's New
- **23 edge types**: Full epistemic, temporal, agent, and failure edge support
- **DQA (Data Quality Analyzer)**: Comprehensive quality scoring endpoint
- **Epistemic labeling**: Node classification (observed, inferred, assumed, recommended)
- **Taxonomy system**: Hierarchical node categorization
- **Session clustering**: Embedding-based episodic clustering
- **Confidence evolution**: PEMS-inspired maturity scoring
- **Capture/Digest/Enrichment pipeline**: Automated knowledge extraction
- **MCP healing tools**: Self-diagnostic and repair capabilities
- **Rate limiting**: MCP request throttling
- **Benchmark suite**: Performance and recall analysis tools

## Files Added/Updated
- internal/config/config.go — Fixed fallback DSN password
- internal/handler/mcp.go — MCP HTTP transport handler
- internal/handler/dqa.go — Data quality analyzer
- internal/handler/capture.go — Session capture endpoint
- internal/handler/digest.go — Knowledge digest endpoint
- internal/handler/enrichment.go — Node enrichment pipeline
- internal/handler/node_epistemic.go — Epistemic label management
- internal/handler/quality.go — Quality scoring
- internal/handler/skills.go — Skills integration
- internal/handler/taxonomy.go — Taxonomy management
- internal/mcp/*.go — MCP server implementation (cache, fluxmem, healing, logging, ratelimit, validation)
- internal/capture/ — Capture pipeline
- internal/digest/ — Digest pipeline
- internal/enrichment/ — Enrichment pipeline
- internal/quality/ — Quality analysis
- internal/skills/ — Skills integration
- internal/taxonomy/ — Taxonomy engine
- scripts/mine_sessions.py — Session mining
- scripts/mine_sessions_fast.py — Fast session mining
- docs/BENCHMARK_AND_RECALL_ANALYSIS.md — Performance analysis
- docs/DASHBOARD_AUDIT_REPORT.md — Dashboard audit findings
- docs/DQA_AUDIT_REPORT.md — DQA audit findings
- docs/FLUXMEM_IMPLEMENTATION_SUMMARY.md — FluxMem architecture
- docs/RELEASE_NOTES_v0.5.2.md — Detailed release notes
- docs/TOOLS_TAB_AUDIT_REPORT.md — Tools tab audit
- mindbank-mcp — Rebuilt binary with fixed DSN
- mindbank-api — Updated API binary

## Quick Start
```bash
bash scripts/setup.sh         # One-time setup
bash scripts/start.sh          # Start API + MCP
systemctl --user enable mindbank-mcp.service  # Auto-start MCP
```

## Auto-Heal
```bash
~/.hermes/scripts/mindbank-autoheal.sh  # Manual heal
systemctl --user restart mindbank-mcp.service  # systemd restart
```
