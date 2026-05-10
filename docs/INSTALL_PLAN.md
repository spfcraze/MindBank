# MindBank Installation Plan — Multi-Platform Support

## Problem

Current `setup.sh` assumes:
- Docker installed (not everyone has it)
- systemd available (macOS doesn't have it)
- Ubuntu/Debian package manager (macOS uses brew)
- Postgres always via Docker (user's instance uses native Postgres)
- Single installation path

## Target Platforms

| Platform | Postgres | Ollama | Service Manager |
|----------|----------|--------|-----------------|
| Linux (Ubuntu/Debian) | Docker or native | native | systemd |
| Linux (Fedora/RHEL) | Docker or native | native | systemd |
| macOS (Intel/ARM) | Docker or brew | brew or native | launchd |
| Windows WSL2 | Docker Desktop | native in WSL | systemd (WSL2) |

## Installation Modes

### Mode 1: Full Docker (easiest)
- Postgres: Docker (pgvector/pgvector:pg16)
- Ollama: Docker (ollama/ollama:latest)
- API: Docker (built from Dockerfile)
- MCP: Native binary (needs to run in Hermes process)
- Best for: First-time users, quick eval

### Mode 2: Hybrid (current default)
- Postgres: Docker
- Ollama: Native install
- API: Native binary
- MCP: Native binary
- Best for: Users who have Ollama already, want performance

### Mode 3: Full Native (best performance)
- Postgres: System package + pgvector compile
- Ollama: Native install
- API: Native binary
- MCP: Native binary
- Best for: Power users, no Docker overhead

### Mode 4: Existing Postgres (user has Postgres already)
- Postgres: Use existing instance, just create database + pgvector
- Ollama: Native install
- API: Native binary
- MCP: Native binary
- Best for: Users with existing Postgres (like your setup)

## Plan

### Step 1: Rewrite setup.sh as interactive wizard

```
./scripts/setup.sh

  MindBank Setup
  ==============

  Detected: Ubuntu 24.04 / x86_64 / Docker 29.1 / systemd 255

  How would you like to run Postgres?
  1) Docker (recommended — easiest, includes pgvector)
  2) Use existing Postgres instance
  3) Install native Postgres + pgvector (advanced)

  How would you like to run Ollama?
  1) Native install (recommended — better performance)
  2) Docker (if you don't want to install Ollama system-wide)

  How would you like to run MindBank API?
  1) Native binary (recommended — best performance)
  2) Docker (easiest, but adds overhead)

  [detected choices shown as defaults]
```

### Step 2: Platform-specific paths

**Linux (apt-based: Ubuntu/Debian)**
```bash
# Postgres native
sudo apt install postgresql-16 postgresql-16-pgvector
sudo -u postgres createdb mindbank
sudo -u postgres psql mindbank -c "CREATE EXTENSION vector;"

# Ollama
curl -fsSL https://ollama.ai/install.sh | sh

# Service
sudo systemctl enable mindbank
```

**Linux (dnf-based: Fedora/RHEL)**
```bash
# Postgres native
sudo dnf install postgresql16-server postgresql16-contrib
# pgvector from source or COPR

# Ollama
curl -fsSL https://ollama.ai/install.sh | sh
```

**macOS (Homebrew)**
```bash
# Postgres native
brew install postgresql@16 pgvector
brew services start postgresql@16
createdb mindbank
psql mindbank -c "CREATE EXTENSION vector;"

# Ollama
brew install ollama
brew services start ollama

# Service (launchd)
cp ~/Library/LaunchAgents/com.mindbank.plist
```

**Windows WSL2**
```bash
# Same as Linux Ubuntu (WSL2 runs Ubuntu)
# Docker Desktop provides Docker + Postgres
# Ollama installed natively in WSL
# systemd works in WSL2
```

### Step 3: Configuration auto-detection

```bash
# Detect existing Postgres
if psql -U postgres -l 2>/dev/null | grep -q mindbank; then
    USE_EXISTING_PG=true
    PG_DSN="postgres://..."
fi

# Detect Docker
if command -v docker &>/dev/null; then
    HAS_DOCKER=true
fi

# Detect Ollama
if command -v ollama &>/dev/null; then
    HAS_OLLAMA=true
fi

# Detect platform
case "$(uname -s)" in
    Linux*)  PLATFORM=linux; SERVICE_CMD=systemctl ;;
    Darwin*) PLATFORM=macos; SERVICE_CMD=launchctl ;;
    *)       PLATFORM=unknown ;;
esac
```

### Step 4: Multiple docker-compose files

```bash
docker-compose.full.yml      # Postgres + Ollama + API (all Docker)
docker-compose.hybrid.yml    # Postgres only (API + Ollama native)
docker-compose.dev.yml       # Postgres + Ollama (API runs locally for dev)
```

### Step 5: Service management per platform

**Linux (systemd)**
```ini
[Unit]
Description=MindBank
After=network-online.target

[Service]
ExecStart=/opt/mindbank/mindbank
Restart=on-failure
Environment=MB_DB_DSN=...
Environment=MB_OLLAMA_URL=http://localhost:11434

[Install]
WantedBy=multi-user.target
```

**macOS (launchd)**
```xml
<?xml version="1.0" encoding="UTF-8"?>
<!DOCTYPE plist PUBLIC "-//Apple//DTD PLIST 1.0//EN">
<plist version="1.0">
<dict>
    <key>Label</key>
    <string>com.mindbank</string>
    <key>ProgramArguments</key>
    <array>
        <string>/usr/local/bin/mindbank</string>
    </array>
    <key>EnvironmentVariables</key>
    <dict>
        <key>MB_DB_DSN</key>
        <string>postgres://mindbank:YOUR_DB_PASSWORD@localhost:5434/mindbank?sslmode=disable</string>
        <key>MB_OLLAMA_URL</key>
        <string>http://localhost:11434</string>
    </dict>
    <key>RunAtLoad</key>
    <true/>
    <key>KeepAlive</key>
    <true/>
</dict>
</plist>
```

**WSL2 (systemd — works in modern WSL)**
```bash
# Same as Linux — WSL2 supports systemd natively
sudo systemctl enable mindbank
```

### Step 6: Pre-built binaries

Release binaries for common platforms:
```
releases/
├── mindbank-linux-amd64
├── mindbank-linux-arm64
├── mindbank-macos-amd64
├── mindbank-macos-arm64
├── mindbank-mcp-linux-amd64
├── mindbank-mcp-linux-arm64
├── mindbank-mcp-macos-amd64
├── mindbank-mcp-macos-arm64
```

Users download the right binary instead of needing Go.

### Step 7: Update Hermes plugin config

The plugin `__init__.py` should auto-detect the API URL:
1. Check `MINDBANK_API_URL` env var
2. Check `~/.hermes/mindbank.json`
3. Try common ports (8095, 8096, 8097)
4. Fall back to default `http://localhost:8095`

### Step 8: pgvector installation options

| Method | When to use |
|--------|-------------|
| Docker pgvector image | Simplest, always works |
| Package manager | `apt install postgresql-16-pgvector` (Ubuntu 24.04+) |
| From source | Older distros without pgvector package |
| Existing Postgres | Just `CREATE EXTENSION vector;` if pgvector available |

## Files to create/modify

```
scripts/setup.sh                 → Rewrite as interactive wizard
scripts/detect-platform.sh       → Platform detection utilities
scripts/install-postgres.sh      → Postgres installation (Docker/native/exists)
scripts/install-ollama.sh        → Ollama installation (native/docker)
scripts/install-service.sh       → Service creation (systemd/launchd)
docker-compose.full.yml          → All-in-Docker setup
docker-compose.hybrid.yml        → Postgres-only Docker (current)
docs/install-linux.md            → Linux-specific instructions
docs/install-macos.md            → macOS-specific instructions
docs/install-wsl.md              → WSL2-specific instructions
docs/install-no-docker.md        → Native-only installation
plugins/memory/mindbank/__init__.py → Auto-detect API URL
```

## Decision needed

1. **Default mode?** — Hybrid (Docker Postgres + native Ollama + native API)?
2. **Pre-built binaries?** — Cross-compile for Linux/macOS ARM/AMD?
3. **Minimum Postgres version?** — pgvector needs Postgres 14+
4. **Minimum Go version?** — Currently 1.23+, should we support older?
5. **Homebrew formula?** — `brew install mindbank` for macOS users?
