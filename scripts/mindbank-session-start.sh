#!/bin/bash
# MindBank SessionStart auto-recall hook.
#
# Injects the most important memories for the current project into the agent's
# context at the start of every session — so recall is automatic instead of
# something the agent has to remember to query.
#
# Claude Code wiring (~/.claude/settings.json):
#   "hooks": {
#     "SessionStart": [
#       { "hooks": [ { "type": "command",
#         "command": "/home/rat/mindbank/scripts/mindbank-session-start.sh" } ] }
#     ]
#   }
#
# Contract: Claude Code passes a JSON object on stdin that includes "cwd".
# We emit JSON with hookSpecificOutput.additionalContext (Claude Code adds it
# to the session context). Any failure -> empty output, never blocks the
# session.

set -euo pipefail

API="${MB_API_URL:-http://127.0.0.1:8095/api/v1}"
WORKSPACE="${MB_WORKSPACE:-hermes}"
MAX_TOKENS="${MB_SNAPSHOT_TOKENS:-1800}"

emit() { # $1 = additionalContext text (may be empty)
  # JSON-encode the text safely via python (handles quotes/newlines/unicode).
  python3 - "$1" <<'PY'
import json, sys
ctx = sys.argv[1] if len(sys.argv) > 1 else ""
print(json.dumps({
    "hookSpecificOutput": {
        "hookEventName": "SessionStart",
        "additionalContext": ctx
    }
}))
PY
}

# Read the hook payload from stdin (non-blocking-ish; empty if none).
payload="$(cat 2>/dev/null || true)"

# Derive cwd: prefer the hook payload's cwd, else $PWD.
cwd="$(printf '%s' "$payload" | python3 -c 'import sys,json;
try:
    d=json.load(sys.stdin); print(d.get("cwd") or "")
except Exception:
    print("")' 2>/dev/null || true)"
[ -z "$cwd" ] && cwd="${PWD:-}"

# Namespace = project folder name (matches MindBank ingestion convention).
namespace=""
if [ -n "$cwd" ]; then
  namespace="$(basename "$cwd" | tr '[:upper:]' '[:lower:]')"
fi

# Bail quietly if the API isn't up — never break the session.
if ! curl -fsS --max-time 2 "$API/health" >/dev/null 2>&1; then
  emit ""
  exit 0
fi

# Fetch the namespace-scoped wake-up snapshot (falls back to workspace-wide).
fetch() { # $1 = namespace (may be empty)
  local url="$API/snapshot?workspace=$WORKSPACE&max_tokens=$MAX_TOKENS"
  [ -n "$1" ] && url="$url&namespace=$1"
  curl -fsS --max-time 5 "$url" 2>/dev/null \
    | python3 -c 'import sys,json;
try:
    d=json.load(sys.stdin); print((d.get("content") or "").strip())
except Exception:
    print("")' 2>/dev/null || true
}

content="$(fetch "$namespace")"
# Keep memories project-scoped: an unknown/new project gets nothing rather
# than a dump of unrelated memories. Set MB_SNAPSHOT_FALLBACK=1 to fall back
# to workspace-wide memories when the project namespace is empty.
if { [ -z "$content" ] || printf '%s' "$content" | grep -q '^No memories'; } \
   && [ "${MB_SNAPSHOT_FALLBACK:-0}" = "1" ]; then
  content="$(fetch "")"
fi

if [ -z "$content" ] || printf '%s' "$content" | grep -q '^No memories'; then
  emit ""
  exit 0
fi

header="# MindBank memory — recalled for this project"
[ -n "$namespace" ] && header="$header ($namespace)"
emit "$header

$content

_(Source: MindBank. Use the mindbank MCP tools — search, ask, create_node — to recall more or store new memories.)_"
