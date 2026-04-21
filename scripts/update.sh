#!/bin/bash
# MindBank Update Script
# Checks GitHub for updates and applies them
#
# Usage:
#   bash update.sh                    # check + prompt to update
#   bash update.sh --check            # check only, exit 0 if up to date, exit 1 if update available
#   bash update.sh --yes              # update without prompting
#   bash update.sh --force            # force update even if same version
#   bash update.sh --plugin-only      # only update the Hermes plugin
#   bash update.sh --no-backup        # skip backup before update
#
# Environment:
#   MINDBANK_DIR      - Install directory (default: auto-detect)
#   GITHUB_REPO       - GitHub repo (default: spfcraze/MindBank)
#   GITHUB_TOKEN      - Optional GitHub token for higher rate limits

set -e

GITHUB_REPO="${GITHUB_REPO:-spfcraze/MindBank}"
GITHUB_API="https://api.github.com/repos/${GITHUB_REPO}"
GITHUB_RAW="https://raw.githubusercontent.com/${GITHUB_REPO}"
AUTO_YES=false
CHECK_ONLY=false
PLUGIN_ONLY=false
FORCE=false
NO_BACKUP=false
NO_RESTART=false

# ---- Parse args ----
for arg in "$@"; do
    case $arg in
        --check) CHECK_ONLY=true ;;
        --yes|-y) AUTO_YES=true ;;
        --force) FORCE=true ;;
        --plugin-only) PLUGIN_ONLY=true ;;
        --no-backup) NO_BACKUP=true ;;
        --no-restart) NO_RESTART=true ;;
        --help|-h)
            echo "Usage: bash update.sh [--check] [--yes] [--force] [--plugin-only] [--no-backup] [--no-restart]"
            exit 0
            ;;
    esac
done

# ---- Auto-detect install directory ----
SCRIPT_DIR="$(cd "$(dirname "$0")" && pwd)"
MINDBANK_DIR="${MINDBANK_DIR:-$(dirname "$SCRIPT_DIR")}"

# If we're in scripts/, go up one level
if [ "$(basename "$MINDBANK_DIR")" = "scripts" ]; then
    MINDBANK_DIR="$(dirname "$MINDBANK_DIR")"
fi

# ---- Helper: GitHub API call ----
gh_api() {
    local url="$1"
    local auth_header=""
    if [ -n "${GITHUB_TOKEN:-}" ]; then
        auth_header="-H Authorization: Bearer ${GITHUB_TOKEN}"
    fi
    curl -sf --connect-timeout 10 --max-time 30 \
        -H "Accept: application/vnd.github.v3+json" \
        $auth_header "$url" 2>/dev/null
}

# ---- Get local version ----
get_local_version() {
    if [ -f "$MINDBANK_DIR/VERSION" ]; then
        cat "$MINDBANK_DIR/VERSION" | tr -d '[:space:]'
    else
        echo "0.0.0"
    fi
}

# ---- Check if git-based install ----
is_git_install() {
    [ -d "$MINDBANK_DIR/.git" ]
}

# ---- Get latest release from GitHub ----
echo "╔══════════════════════════════════════════════════╗"
echo "║  MindBank Updater                                ║"
echo "╚══════════════════════════════════════════════════╝"
echo ""

LOCAL_VERSION=$(get_local_version)
echo "  Local version:  ${LOCAL_VERSION}"
echo "  Install dir:    ${MINDBANK_DIR}"
echo "  Install type:   $(is_git_install && echo 'git' || echo 'tarball')"
echo ""
echo "  Checking GitHub for updates..."

RELEASE_JSON=$(gh_api "${GITHUB_API}/releases/latest" 2>/dev/null || echo "")

if [ -z "$RELEASE_JSON" ] || echo "$RELEASE_JSON" | grep -q '"message":"Not Found"'; then
    # No releases — try tags
    TAGS_JSON=$(gh_api "${GITHUB_API}/tags" 2>/dev/null || echo "")
    if [ -z "$TAGS_JSON" ] || [ "$TAGS_JSON" = "[]" ] || echo "$TAGS_JSON" | grep -q '"message":"Not Found"'; then
        echo ""
        echo "  ERROR: No releases or tags found on GitHub."
        echo "  Repository: https://github.com/${GITHUB_REPO}"
        echo ""
        echo "  The maintainer needs to create a release first:"
        echo "    1. git tag v0.1.0"
        echo "    2. git push origin v0.1.0"
        echo "    3. gh release create v0.1.0 --title 'v0.1.0' --notes 'Initial release'"
        exit 1
    fi
    # Parse first tag
    REMOTE_VERSION=$(echo "$TAGS_JSON" | python3 -c "
import json, sys
try:
    tags = json.load(sys.stdin)
    print(tags[0]['name'].lstrip('v'))
except:
    print('unknown')
" 2>/dev/null || echo "unknown")
    RELEASE_DATE="unknown"
    RELEASE_URL="https://github.com/${GITHUB_REPO}/releases"
    CHANGELOG="No release notes. Check commits for changes."
    TARBALL_URL="https://github.com/${GITHUB_REPO}/archive/refs/tags/v${REMOTE_VERSION}.tar.gz"
else
    # Parse release info
    REMOTE_VERSION=$(echo "$RELEASE_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    tag = d.get('tag_name', d.get('name', ''))
    print(tag.lstrip('v'))
except:
    print('unknown')
" 2>/dev/null || echo "unknown")

    RELEASE_DATE=$(echo "$RELEASE_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    date = d.get('published_at', d.get('created_at', ''))
    print(date[:10] if date else 'unknown')
except:
    print('unknown')
" 2>/dev/null || echo "unknown")

    RELEASE_URL=$(echo "$RELEASE_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    print(d.get('html_url', 'https://github.com/${GITHUB_REPO}/releases'))
except:
    print('https://github.com/${GITHUB_REPO}/releases')
" 2>/dev/null || echo "https://github.com/${GITHUB_REPO}/releases")

    CHANGELOG=$(echo "$RELEASE_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    body = d.get('body', '')
    if len(body) > 500:
        body = body[:497] + '...'
    print(body)
except:
    print('No changelog available.')
" 2>/dev/null || echo "No changelog available.")

    TARBALL_URL=$(echo "$RELEASE_JSON" | python3 -c "
import json, sys
try:
    d = json.load(sys.stdin)
    print(d.get('tarball_url', ''))
except:
    print('')
" 2>/dev/null || echo "")
fi

echo "  Remote version: ${REMOTE_VERSION} (${RELEASE_DATE})"
echo ""

# ---- Compare versions ----
NEEDS_UPDATE=false
if [ "$LOCAL_VERSION" = "0.0.0" ]; then
    echo "  Status: LEGACY INSTALL (pre-versioning)"
    NEEDS_UPDATE=true
elif [ "$LOCAL_VERSION" != "$REMOTE_VERSION" ]; then
    echo "  Status: UPDATE AVAILABLE"
    NEEDS_UPDATE=true
else
    echo "  Status: UP TO DATE"
fi

if [ "$FORCE" = true ]; then
    NEEDS_UPDATE=true
    echo "  (forced update)"
fi

echo ""

if [ "$NEEDS_UPDATE" = true ]; then
    echo "  Changelog:"
    echo "  ─────────────────────────────────────────"
    echo "$CHANGELOG" | sed 's/^/  /'
    echo "  ─────────────────────────────────────────"
    echo ""
    echo "  Release: ${RELEASE_URL}"
    echo ""
fi

# ---- Check-only mode ----
if [ "$CHECK_ONLY" = true ]; then
    if [ "$NEEDS_UPDATE" = true ]; then
        echo '{"needs_update":true,"local":"'"$LOCAL_VERSION"'","remote":"'"$REMOTE_VERSION"'","date":"'"$RELEASE_DATE"'"}'
        exit 1
    else
        echo '{"needs_update":false,"local":"'"$LOCAL_VERSION"'","remote":"'"$REMOTE_VERSION"'"}'
        exit 0
    fi
fi

if [ "$NEEDS_UPDATE" = false ]; then
    echo "  Nothing to do."
    exit 0
fi

# ---- Prompt ----
if [ "$AUTO_YES" = false ]; then
    read -p "  Update from ${LOCAL_VERSION} to ${REMOTE_VERSION}? (y/N) " -n 1 -r
    echo
    if [[ ! $REPLY =~ ^[Yy]$ ]]; then
        echo "  Cancelled."
        exit 0
    fi
fi

# ---- Backup ----
if [ "$NO_BACKUP" = false ]; then
    BACKUP_DIR="$HOME/.mindbank/backup/$(date +%Y%m%d-%H%M%S)"
    echo ""
    echo "  Backing up to: $BACKUP_DIR"
    mkdir -p "$BACKUP_DIR"
    [ -f "$MINDBANK_DIR/mindbank-api" ] && cp "$MINDBANK_DIR/mindbank-api" "$BACKUP_DIR/"
    [ -f "$MINDBANK_DIR/mindbank-mcp" ] && cp "$MINDBANK_DIR/mindbank-mcp" "$BACKUP_DIR/"
    [ -f "$MINDBANK_DIR/VERSION" ] && cp "$MINDBANK_DIR/VERSION" "$BACKUP_DIR/"
    echo "  Backup complete ✓"
fi

# ---- Update ----
echo ""
echo "  Updating..."

if is_git_install; then
    echo "  Method: git pull"
    cd "$MINDBANK_DIR"
    git fetch origin
    git reset --hard "origin/main" 2>/dev/null || git reset --hard "origin/master" 2>/dev/null || git pull --ff-only
    echo "  Git pull complete ✓"
else
    echo "  Method: tarball download"
    if [ -z "$TARBALL_URL" ]; then
        echo "  ERROR: No tarball URL available. Try cloning the repo instead:"
        echo "    git clone https://github.com/${GITHUB_REPO}.git $MINDBANK_DIR"
        exit 1
    fi

    TMPDIR=$(mktemp -d)
    echo "  Downloading..."
    curl -sfL --connect-timeout 10 --max-time 60 "$TARBALL_URL" -o "$TMPDIR/mindbank.tar.gz"
    echo "  Extracting..."
    tar -xzf "$TMPDIR/mindbank.tar.gz" -C "$TMPDIR"

    # Find extracted dir (GitHub tarballs have a root dir like spfcraze-MindBank-abc1234)
    EXTRACTED=$(find "$TMPDIR" -maxdepth 1 -type d \( -name "*MindBank*" -o -name "*mindbank*" \) | head -1)
    if [ -z "$EXTRACTED" ]; then
        EXTRACTED=$(ls -d "$TMPDIR"/*/ 2>/dev/null | head -1)
    fi

    if [ -z "$EXTRACTED" ]; then
        echo "  ERROR: Could not find extracted directory."
        rm -rf "$TMPDIR"
        exit 1
    fi

    # Copy files (preserve .env and data)
    rsync -a --exclude='.env' --exclude='.git' --exclude='mindbank-api' --exclude='mindbank-mcp' \
        "$EXTRACTED/" "$MINDBANK_DIR/"
    rm -rf "$TMPDIR"
    echo "  Tarball update complete ✓"
fi

# ---- Post-update cleanup: remove duplicate analyze.go ----
if [ -f "$MINDBANK_DIR/internal/handler/analyze.go" ] && [ -f "$MINDBANK_DIR/internal/handler/analyze_common.go" ]; then
    echo "  Cleaning up duplicate analyze.go..."
    rm -f "$MINDBANK_DIR/internal/handler/analyze.go"
    echo "  Cleanup complete ✓"
fi

# ---- Rebuild ----
if [ "$PLUGIN_ONLY" = false ]; then
    if command -v go &>/dev/null; then
        echo ""
        echo "  Building API server..."
        cd "$MINDBANK_DIR"
        go build -o mindbank-api ./cmd/mindbank
        echo "  Built: mindbank-api ✓"

        if [ -f "$MINDBANK_DIR/cmd/mindbank-mcp/main.go" ]; then
            echo "  Building MCP server..."
            go build -o mindbank-mcp ./cmd/mindbank-mcp
            echo "  Built: mindbank-mcp ✓"
        fi
    else
        echo "  WARNING: Go not found. Skipping binary rebuild."
        echo "  Install Go: https://go.dev/dl/"
    fi
fi

# ---- Update plugin ----
echo ""
echo "  Updating integrations..."
if [ -f "$MINDBANK_DIR/scripts/install-plugin.sh" ]; then
    export MINDBANK_MCP_BIN="$MINDBANK_DIR/mindbank-mcp"
    bash "$MINDBANK_DIR/scripts/install-plugin.sh" --all 2>/dev/null || true
    echo "  Plugin/MCP configs refreshed ✓"
else
    echo "  WARNING: install-plugin.sh not found, skipping integration update."
fi

# ---- Update VERSION file ----
echo "$REMOTE_VERSION" > "$MINDBANK_DIR/VERSION"

# ---- Auto-restart ----
restart_ok=false
if [ "$NO_RESTART" = false ] && [ "$PLUGIN_ONLY" = false ]; then
    echo ""
    echo "  Restarting MindBank API..."

    # Check if running as systemd service
    if systemctl --user is-active mindbank &>/dev/null; then
        echo "  Detected: systemd user service"
        systemctl --user restart mindbank
        sleep 2
        if systemctl --user is-active mindbank &>/dev/null; then
            echo "  systemd restart ✓"
            restart_ok=true
        else
            echo "  ERROR: systemd restart failed. Check: journalctl --user -u mindbank"
        fi
    elif systemctl is-active mindbank &>/dev/null; then
        echo "  Detected: systemd system service"
        sudo systemctl restart mindbank
        sleep 2
        if systemctl is-active mindbank &>/dev/null; then
            echo "  systemd restart ✓"
            restart_ok=true
        else
            echo "  ERROR: systemd restart failed. Check: journalctl -u mindbank"
        fi
    else
        # Running as standalone process — find and kill, then restart
        PID=$(pgrep -f "mindbank-api" 2>/dev/null || pgrep -x "mindbank" 2>/dev/null || pgrep -f "./mindbank" 2>/dev/null)
        if [ -n "$PID" ]; then
            echo "  Found process PID: $PID"
            kill "$PID" 2>/dev/null
            sleep 2
            # Force kill if still running
            if kill -0 "$PID" 2>/dev/null; then
                kill -9 "$PID" 2>/dev/null
                sleep 1
            fi
            echo "  Stopped old process ✓"
        fi

        # Start new process
        cd "$MINDBANK_DIR"
        if [ -f ".env" ]; then
            source .env
        fi
        export MB_DB_DSN="${MB_DB_DSN:-postgres://mindbank:mindbank_secret@localhost:5432/mindbank?sslmode=disable}"
        export MB_OLLAMA_URL="${MB_OLLAMA_URL:-http://localhost:11434}"
        export MB_PORT="${MB_PORT:-8095}"

        nohup ./mindbank-api >> /tmp/mindbank.log 2>&1 &
        NEW_PID=$!
        sleep 2

        # Verify it started
        if kill -0 "$NEW_PID" 2>/dev/null; then
            if curl -sf "http://localhost:${MB_PORT}/api/v1/health" &>/dev/null; then
                echo "  Started PID: $NEW_PID ✓"
                echo "  Health check: OK ✓"
                restart_ok=true
            else
                echo "  WARNING: Process started but health check failed."
                echo "  Check logs: tail /tmp/mindbank.log"
            fi
        else
            echo "  ERROR: Process failed to start."
            echo "  Check logs: tail /tmp/mindbank.log"
        fi
    fi
else
    if [ "$NO_RESTART" = true ]; then
        echo ""
        echo "  Auto-restart skipped (--no-restart)"
    fi
fi

# ---- Done ----
echo ""
echo "══════════════════════════════════════════════"
echo ""
echo "  Updated: ${LOCAL_VERSION} → ${REMOTE_VERSION}"
echo ""
if [ "$NO_BACKUP" = false ]; then
    echo "  Backup at: $BACKUP_DIR"
fi
if [ "$restart_ok" = true ]; then
    echo "  MindBank API restarted and healthy ✓"
elif [ "$NO_RESTART" = false ] && [ "$PLUGIN_ONLY" = false ]; then
    echo "  Restart failed. Start manually:"
    echo "    cd $MINDBANK_DIR && make run"
fi
echo ""
