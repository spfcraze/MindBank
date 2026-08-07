#!/bin/bash
# hermes-mind — launch Hermes with the current directory as the MindBank
# project namespace, so memories from THIS session land in the right project
# even when hundreds of Hermes sessions run concurrently.
#
# Usage:  cd ~/<project> && hermes-mind [hermes args...]
# Install (automatic for your normal workflow):
#     echo "alias hermes=hermes-mind" >> ~/.bashrc
#
# Sets MINDBANK_NAMESPACE, which Hermes sends to MindBank MCP as the
# X-Mindbank-Namespace header on every tool call (see mcp_servers.mindbank
# headers in ~/.hermes/config.yaml). The MCP server then tags every memory
# created by this session with the project namespace.

set -e

# Resolve the hermes entrypoint (override with HERMES_BIN).
HERMES_BIN="${HERMES_BIN:-}"
if [ -z "$HERMES_BIN" ]; then
    if command -v hermes >/dev/null 2>&1; then
        HERMES_BIN="$(command -v hermes)"
    elif [ -x "$HOME/hermes-agent/hermes" ]; then
        HERMES_BIN="$HOME/hermes-agent/hermes"
    else
        echo "hermes-mind: cannot find hermes (set HERMES_BIN)" >&2
        exit 1
    fi
fi

# Project name = basename of the launch directory, unless it is a generic
# home/system dir (those would produce meaningless namespaces).
proj="$(basename "$(pwd)")"
lower="$(echo "$proj" | tr '[:upper:]' '[:lower:]')"
case " $lower " in
    *" rat "*|*" home "*|*" hermes "*|*" wsl "*|*" mnt "*|*" tmp "*|*" root "*|*" . "*|*" ~ "*)
        proj=""
        ;;
esac
if [ -n "$proj" ]; then
    # Lowercase, keep alnum/dash/underscore only.
    proj="$(echo "$proj" | tr '[:upper:]' '[:lower:]' | tr -cd 'a-z0-9_-')"
    export MINDBANK_NAMESPACE="$proj"
    echo "hermes-mind: namespace=$proj" >&2
else
    unset MINDBANK_NAMESPACE 2>/dev/null || true
fi

exec "$HERMES_BIN" "$@"
