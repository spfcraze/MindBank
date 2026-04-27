.PHONY: build run stop db-up db-down db-status logs tidy vet clean setup install-mcp stop-mcp update version

# === MindBank Commands ===

# Run setup wizard
setup:
	bash scripts/setup.sh

# Build the binary
build:
	go build -o mindbank-api ./cmd/mindbank

# Start everything (Postgres + API)
run: build db-up
	@echo "Starting MindBank API..."
	@pkill -x "./mindbank-api" 2>/dev/null || true
	@sleep 1
	@MB_DB_DSN="postgres://mindbank:***@localhost:5434/mindbank?sslmode=disable" \
		MB_OLLAMA_URL="http://localhost:11434" \
		MB_PORT=8095 \
		nohup ./mindbank-api >> mindbank.log 2>&1 &
	@sleep 2
	@curl -s http://localhost:8095/api/v1/health
	@echo ""
	@echo "Dashboard: http://localhost:8095"

# Stop everything
stop:
	@pkill -x "./mindbank-api" 2>/dev/null && echo "API stopped" || echo "API not running"
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
	tail -f mindbank.log

clean:
	rm -f mindbank-api mindbank-mcp
	docker compose down -v

# === MCP Server ===

build-mcp:
	go build -o mindbank-mcp ./cmd/mindbank-mcp

install-mcp: build-mcp
	@echo "Installing MindBank MCP systemd service..."
	@mkdir -p ~/.config/systemd/user
	@cp scripts/mindbank-mcp.service ~/.config/systemd/user/
	@systemctl --user daemon-reload
	@systemctl --user enable mindbank-mcp
	@systemctl --user start mindbank-mcp
	@echo "MCP server installed and started."
	@echo "Check status: systemctl --user status mindbank-mcp"
	@echo "Check health: curl -s http://127.0.0.1:8096/health"

stop-mcp:
	@systemctl --user stop mindbank-mcp 2>/dev/null || true
	@echo "MCP server stopped."

# === Version + Updates ===

version:
	@cat VERSION

update:
	bash scripts/update.sh

# === Quick health check ===

health:
	@curl -s http://localhost:8095/api/v1/health | python3 -m json.tool 2>/dev/null || curl -s http://localhost:8095/api/v1/health
