# MindBank vv0.5.1-39-g2bb448e

## What's Fixed
- **Password/DSN mismatch**: .env now correctly sets MB_POSTGRES_PASSWORD and MB_DB_DSN
- **MCP auto-start**: scripts/start.sh now launches both API (8095) and MCP (8096) servers
- **Systemd services**: install-service.sh creates mindbank-api.service + mindbank-mcp.service
- **Setup script**: setup.sh generates correct .env with all required variables
- **23 edge types**: Full epistemic, temporal, agent, and failure edge support

## Files Added/Updated
- scripts/start.sh — Unified start script (API + MCP)
- scripts/install-service.sh — Systemd user service installer
- scripts/setup.sh — One-command setup with correct .env generation
- scripts/install-plugin.sh — Fixed default DSN
- scripts/mindbank.service — Updated template
- scripts/mindbank-mcp.service — New MCP service template
- .env — Complete configuration template
- README.md — Updated install instructions

## Quick Start
```bash
bash scripts/setup.sh    # One-time setup
bash scripts/start.sh    # Start everything
```
