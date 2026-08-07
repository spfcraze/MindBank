#!/bin/bash
# Hermes Session Hook - Auto-mine sessions into MindBank
# 
# Add this to your shell profile or tmux configuration:
#   export HERMES_SESSION_HOOK="$HOME/mindbank/scripts/hermes-session-hook.sh"
#
# Or for tmux, add to ~/.tmux.conf:
#   set-hook -g session-closed 'run-shell "$HOME/mindbank/scripts/hermes-session-hook.sh #S"'

set -e

# Configuration
MIND_BANK_API="${MIND_BANK_API:-http://127.0.0.1:8095/api/v1}"
AUTO_MINER="${AUTO_MINER:-$(CDPATH= cd -- "$(dirname -- "$0")" && pwd)/auto_miner.py}"
SESSION_DIR="${HERMES_SESSION_DIR:-$HOME/.hermes/sessions}"
LOG_FILE="${MIND_BANK_LOG:-/tmp/mindbank-auto-capture.log}"

# Log function
log() {
    echo "[$(date '+%Y-%m-%d %H:%M:%S')] $1" >> "$LOG_FILE"
}

# Check if MindBank API is available
check_api() {
    if ! curl -s "$MIND_BANK_API/health" > /dev/null 2>&1; then
        log "ERROR: MindBank API not available at $MIND_BANK_API"
        return 1
    fi
    return 0
}

# Mine a single session file
mine_session() {
    local session_file="$1"
    
    if [[ ! -f "$session_file" ]]; then
        log "ERROR: Session file not found: $session_file"
        return 1
    fi
    
    log "Mining session: $session_file"
    
    if python3 "$AUTO_MINER" "$session_file" --api "$MIND_BANK_API" >> "$LOG_FILE" 2>&1; then
        log "SUCCESS: Mined $session_file"
        return 0
    else
        log "ERROR: Failed to mine $session_file"
        return 1
    fi
}

# Mine all recent sessions
mine_all_recent() {
    local since_minutes="${1:-60}"
    local since_epoch=$(($(date +%s) - since_minutes * 60))
    
    log "Mining sessions modified in last $since_minutes minutes"
    
    find "$SESSION_DIR" -type f -name "*.md" -newermt "@$since_epoch" 2>/dev/null | while read -r session_file; do
        mine_session "$session_file"
    done
}

# Main execution
main() {
    log "=== Hermes Session Hook Started ==="
    
    # Check API availability
    if ! check_api; then
        exit 1
    fi
    
    # If called with a specific session file
    if [[ -n "$1" ]]; then
        if [[ -f "$1" ]]; then
            mine_session "$1"
        else
            # Assume it's a session name, look for it
            mine_session "$SESSION_DIR/$1.md"
        fi
    else
        # Mine all recent sessions (last 24 hours)
        mine_all_recent 1440
    fi
    
    log "=== Hermes Session Hook Complete ==="
}

# Run main
main "$@"
