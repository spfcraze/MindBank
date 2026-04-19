.PHONY: build run stop db-up db-down db-status logs tidy vet clean setup install-mcp update version

# === MindBank Commands ===

# Run setup wizard
setup:
	bash scripts/setup.sh

# Build the binary
build:
	go build -o mindbank ./cmd/mindbank

# Start everything (Postgres + API)
run: build db-up
	@echo "Starting MindBank API..."
	@pkill -x "./mindbank" 2>/dev/null || true
	@sleep 1
	@MB_DB_DSN="postgres://mindbank:$${MB_POSTGRES_PASSWORD:-mindbank_secret}@localhost:$${MB_PG_PORT:-5434}/mindbank?sslmode=disable" \
		MB_OLLAMA_URL="http://localhost:$${MB_OLLAMA_PORT:-11434}" \
		MB_PORT=$${MB_PORT:-8095} \
		nohup ./mindbank >> /tmp/mindbank.log 2>&1 &
	@sleep 2
	@curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health || echo "Starting..."
	@echo ""
	@echo "Dashboard: http://localhost:$${MB_PORT:-8095}"

# Stop everything
stop:
	@pkill -x "./mindbank" 2>/dev/null && echo "API stopped" || echo "API not running"
	@$(MAKE) db-down

# === Database (Docker) ===

db-up:
	@echo "Starting Postgres..."
	docker compose up -d --wait
	@echo "Postgres ready."
	docker compose ps

db-down:
	docker compose down

db-status:
	docker compose ps

db-logs:
	docker compose logs --tail=50 -f

# === Development ===

tidy:
	go mod tidy

vet:
	go vet ./...

test:
	go test ./... -v

logs:
	tail -f /tmp/mindbank.log

clean:
	rm -f mindbank mindbank-mcp
	docker compose down -v

health: ## Health check
	@curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health | python3 -m json.tool 2>/dev/null || curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health

build-mcp:
	go build -o mindbank-mcp ./cmd/mindbank-mcp

# === Version + Updates ===

docker-run: ## Run with Docker Compose
	docker compose up -d
	@echo "Dashboard: http://localhost:$${MB_PORT:-8095}"

update:
	bash scripts/update.sh

# === Quick health check ===

health:
	@curl -s http://localhost:8095/api/v1/health | python3 -m json.tool 2>/dev/null || curl -s http://localhost:8095/api/v1/health
