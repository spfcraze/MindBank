# Troubleshooting

## Port already in use

**Error:** `bind: address already in use` on port 8095 or 5434.

```bash
# Find what's using the port
lsof -i :8095
lsof -i :5434

# Kill it or change the port in .env
MB_PORT=8096
MB_PG_PORT=5435
```

## Docker not running

**Error:** `Cannot connect to the Docker daemon`

```bash
# Start Docker
sudo systemctl start docker

# Or on macOS
open -a Docker
```

## Ollama not connected

**Error:** Health check shows `"ollama":"disconnected"`

```bash
# Install Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Pull the embedding model
ollama pull nomic-embed-text

# Verify
curl http://localhost:11434/api/tags
```

## Postgres won't start

**Error:** Docker container exits immediately.

```bash
# Check logs
docker compose logs

# Common fix: remove old volume
docker compose down -v
docker compose up -d
```

## Binary won't build

**Error:** `go: cannot find main module`

```bash
# Make sure you're in the project root
cd mindbank
go mod tidy
make build
```

## MCP server not connecting to Hermes

**Error:** Hermes doesn't show MindBank tools.

1. Verify the MCP binary path in `~/.hermes/config.yaml`
2. Check the DSN is correct (port, password)
3. Test the binary directly:
```bash
MB_DB_DSN="postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable" ./mindbank-mcp
```
You should see JSON-RPC initialization output.

## Search returns no results

**Possible causes:**
- Namespace filter doesn't match any nodes — try without `namespace` param
- Ollama isn't running — hybrid search needs embeddings
- Database is empty — create some nodes first

```bash
# Check how many nodes exist
curl http://localhost:8095/api/v1/nodes?limit=1

# Test without namespace filter
curl "http://localhost:8095/api/v1/search?q=test"
```

## Slow performance

**Possible causes:**
- First embedding request is slow (model loading) — subsequent requests are fast
- Large graph (>1000 nodes) — `/graph` endpoint may be slow

```bash
# Check latency
curl -w "Time: %{time_total}s\n" http://localhost:8095/api/v1/health
```

## Data not persisting

**Error:** Data lost after restart.

```bash
# Check Docker volume exists
docker volume ls | grep mindbank

# If missing, your .env might point to a different compose file
cat .env | grep DB_DSN
```

## Migrations not running

**Error:** Database tables don't exist or missing columns.

```bash
# Run migrations manually
make migrate

# Or with Docker
docker exec -i mindbank-postgres psql -U mindbank -d mindbank < internal/db/migrations/003_nodes.sql

# Check if migrations ran
docker exec mindbank-postgres psql -U mindbank -d mindbank -c "\dt"
```
