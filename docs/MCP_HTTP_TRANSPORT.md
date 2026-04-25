MindBank MCP Server — HTTP Transport Mode

## Overview

The MindBank MCP server supports two transport modes:

1. **stdio** (default) — For MCP clients that connect via stdin/stdout
2. **HTTP** — For MCP clients that connect via HTTP (e.g., Hermes with HTTP transport)

## Why HTTP Mode?

Hermes' native MCP stdio transport has a `ClosedResourceError` bug where the connection
drops after tool discovery. The server works fine (verified via `mcporter` CLI), but
Hermes' stdio client transport fails.

HTTP mode bypasses this entirely.

## Usage

### Start HTTP Mode

```bash
cd /home/rat/mindbank
./mindbank-mcp --transport http --http-port 8096
```

Or set env vars:
```bash
export MCP_TRANSPORT=http
export MCP_HTTP_PORT=8096
./mindbank-mcp
```

### Configure Hermes

In `~/.hermes/config.yaml`:

```yaml
mcp_servers:
  mindbank:
    url: http://127.0.0.1:8096/mcp  # HTTP transport
    # command: /home/rat/mindbank/mindbank-mcp  # stdio transport (broken with Hermes)
    timeout: 30
    enabled: true
```

### Test

```bash
hermes mcp test mindbank
```

## HTTP Endpoints

The HTTP server exposes these endpoints:

| Endpoint | Method | Description |
|----------|--------|-------------|
| `/mcp` | POST | MCP JSON-RPC endpoint |
| `/mcp/initialize` | POST | Initialize connection |
| `/mcp/tools/list` | GET | List available tools |
| `/mcp/tools/call` | POST | Call a tool |
| `/health` | GET | Health check |

## Request Format

POST to `/mcp` with JSON-RPC 2.0 body:

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "method": "tools/call",
  "params": {
    "name": "search",
    "arguments": {
      "query": "Polymarket",
      "limit": 5
    }
  }
}
```

## Response Format

```json
{
  "jsonrpc": "2.0",
  "id": 1,
  "result": {
    "content": [
      {
        "type": "text",
        "text": "- [fact] Polymarket CLOB+Data APIs are FREE..."
      }
    ],
    "isError": false
  }
}
```

## Fallback: mcporter CLI

If HTTP mode also has issues, use `mcporter` directly:

```bash
# List tools
npx mcporter list --stdio "./mindbank-mcp" --name mindbank

# Call search
npx mcporter call --stdio "./mindbank-mcp" search query="test" limit=5

# Call create_node
npx mcporter call --stdio "./mindbank-mcp" create_node label="Test" type="fact" content="..."
```

## Implementation Notes

The HTTP transport is implemented in `internal/mcp/http_server.go`.
It reuses the same tool handlers as the stdio server but wraps them
in an HTTP handler with proper JSON-RPC request/response handling.

## Stdio Mode Still Works

stdio mode is NOT removed. It still works with:
- `mcporter` CLI
- Claude Desktop
- Cursor
- Any MCP client with proper stdio transport

Only Hermes' native stdio transport is affected.
