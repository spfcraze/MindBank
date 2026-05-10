# MindBank Quick Start

## Prerequisites
- Docker & Docker Compose
- curl
- Ollama (for embeddings)

## 1. Start Postgres
```bash
docker compose up -d
```

## 2. Start MindBank (API + MCP)
```bash
bash scripts/start.sh
```

This starts:
- API server on http://localhost:8095
- MCP server on http://localhost:8096/mcp

## 3. Verify
```bash
curl http://localhost:8095/api/v1/health
curl http://localhost:8096/mcp
```

## 4. Open Dashboard
http://localhost:8095

## 5. Auto-Start on Boot (optional)
```bash
bash scripts/install-service.sh
systemctl --user start mindbank.target
```

## Configuration
Edit `.env` to change passwords, ports, or model settings.

## Edge Types (23 total)
The system supports 23 typed edges for rich graph relationships:
- Causal: depends_on, learned_from, decided_by, produced, supports, contradicts
- Epistemic: tested_by, invalidated_by, derived_from, assumed
- Temporal: superseded_by, refined_by, merged_into
- Agent: created_by, reviewed_by, executed_by
- Failure: failed_due_to, precondition_for, incompatible_with
- Structural: contains, relates_to, temporal_next, mentions, participated_in
