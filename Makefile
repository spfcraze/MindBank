.PHONY: build run stop db-up db-down db-status logs tidy vet clean help setup migrate

# === MindBank ===

help: ## Show this help
	@grep -E '^[a-zA-Z_-]+:.*?## .*$$' $(MAKEFILE_LIST) | sort | awk 'BEGIN {FS = ":.*?## "}; {printf "\033[36m%-20s\033[0m %s\n", $$1, $$2}'

setup: ## Complete setup (one command)
	@bash scripts/setup.sh

build: ## Build the binary
	go build -o mindbank ./cmd/mindbank
	go build -o mindbank-mcp ./cmd/mindbank-mcp

run: db-up ## Start everything (Postgres + API)
	@echo "Starting MindBank API..."
	@pkill -f "./mindbank" 2>/dev/null || true
	@sleep 1
	@MB_DB_DSN="postgres://mindbank:$${MB_POSTGRES_PASSWORD:-mindbank_secret}@localhost:$${MB_PG_PORT:-5434}/mindbank?sslmode=disable" \
		MB_OLLAMA_URL="http://localhost:$${MB_OLLAMA_PORT:-11434}" \
		MB_PORT=$${MB_PORT:-8095} \
		nohup ./mindbank >> /tmp/mindbank.log 2>&1 &
	@sleep 2
	@curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health || echo "Starting..."
	@echo ""
	@echo "Dashboard: http://localhost:$${MB_PORT:-8095}"

stop: ## Stop everything
	@pkill -f "./mindbank" 2>/dev/null && echo "API stopped" || echo "API not running"
	@$(MAKE) db-down

# === Database ===

db-up: ## Start Postgres
	docker compose up -d postgres
	@echo "Waiting for Postgres..."
	@sleep 3
	docker compose ps

db-down: ## Stop Postgres
	docker compose down

db-status: ## Show Postgres status
	docker compose ps

db-logs: ## Show Postgres logs
	docker compose logs --tail=50 -f

migrate: ## Run database migrations
	@echo "Running migrations..."
	@for f in internal/db/migrations/*.sql; do \
		echo "  $$f"; \
		docker exec -i mindbank-postgres psql -U mindbank -d mindbank < $$f; \
	done
	@echo "Migrations complete"

# === Development ===

tidy: ## Tidy Go modules
	go mod tidy

vet: ## Run go vet
	go vet ./...

test: ## Run tests
	go test ./... -v

logs: ## Show API logs
	tail -f /tmp/mindbank.log

clean: ## Remove binary and volumes
	rm -f mindbank mindbank-mcp
	docker compose down -v

health: ## Health check
	@curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health | python3 -m json.tool 2>/dev/null || curl -s http://localhost:$${MB_PORT:-8095}/api/v1/health

# === Docker ===

docker-build: ## Build Docker image
	docker build -t mindbank .

docker-run: ## Run with Docker Compose
	docker compose up -d
	@echo "Dashboard: http://localhost:$${MB_PORT:-8095}"

docker-stop: ## Stop Docker Compose
	docker compose down
