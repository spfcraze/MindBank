"""MindBank memory plugin — MemoryProvider for graph-structured persistent memory.

Provides semantic search, temporal versioning, graph traversal, and pre-computed
wake-up context via the MindBank HTTP API.

Tools exposed:
  mindbank_store    — store a decision, fact, preference, or problem
  mindbank_search   — hybrid FTS + semantic search
  mindbank_ask      — natural language query
  mindbank_snapshot — wake-up context

Config: $HERMES_HOME/mindbank.json or environment variables.
"""

from __future__ import annotations

import json
import logging
import os
import threading
from typing import Any, Dict, List, Optional
from urllib import request, error

from agent.memory_provider import MemoryProvider

logger = logging.getLogger(__name__)

# Default API URL
DEFAULT_API_URL = "http://localhost:8095/api/v1"

# Tool schemas for the agent

STORE_SCHEMA = {
    "name": "mindbank_store",
    "description": (
        "Store a decision, fact, preference, problem, or advice in MindBank's "
        "graph memory. Creates a node that persists across sessions."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "label": {
                "type": "string",
                "description": "Short name for this memory (e.g., 'Use JWT for auth')",
            },
            "type": {
                "type": "string",
                "enum": ["decision", "fact", "preference", "problem", "advice", "project", "topic", "event", "person", "agent", "concept"],
                "description": "Type of memory node",
            },
            "content": {
                "type": "string",
                "description": "Full description of the memory",
            },
            "summary": {
                "type": "string",
                "description": "One-line summary",
            },
            "namespace": {
                "type": "string",
                "description": "Project namespace (e.g., 'klixsor', 'hermes'). Default: 'global'",
            },
        },
        "required": ["label", "type"],
    },
}

SEARCH_SCHEMA = {
    "name": "mindbank_search",
    "description": (
        "Search MindBank's graph memory using hybrid full-text + semantic search. "
        "Returns relevant nodes ranked by relevance."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "What to search for",
            },
            "namespace": {
                "type": "string",
                "description": "Filter by project namespace",
            },
            "limit": {
                "type": "integer",
                "description": "Max results (default: 5)",
            },
        },
        "required": ["query"],
    },
}

ASK_SCHEMA = {
    "name": "mindbank_ask",
    "description": (
        "Ask a natural language question about past decisions, configurations, "
        "or project knowledge. Returns relevant context from MindBank."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "query": {
                "type": "string",
                "description": "Your question about past knowledge",
            },
        },
        "required": ["query"],
    },
}

SNAPSHOT_SCHEMA = {
    "name": "mindbank_snapshot",
    "description": (
        "Get a pre-computed wake-up context — a compact summary of the most "
        "important facts, decisions, and preferences. Use at session start."
    ),
    "parameters": {
        "type": "object",
        "properties": {},
        "required": [],
    },
}

NEIGHBORS_SCHEMA = {
    "name": "mindbank_neighbors",
    "description": (
        "Get nodes connected to a specific memory node. Use to explore "
        "related memories and understand graph structure."
    ),
    "parameters": {
        "type": "object",
        "properties": {
            "node_id": {
                "type": "string",
                "description": "Node ID to find connections for",
            },
            "depth": {
                "type": "integer",
                "description": "Traversal depth (default: 1, max: 3)",
            },
        },
        "required": ["node_id"],
    },
}


def _api_call(base_url: str, method: str, path: str, body: dict = None, timeout: int = 10) -> dict:
    """Make an HTTP call to the MindBank API."""
    url = base_url.rstrip("/") + path
    data = json.dumps(body).encode() if body else None
    req = request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with request.urlopen(req, timeout=timeout) as resp:
            return json.loads(resp.read())
    except error.URLError as e:
        logger.warning("MindBank API error: %s", e)
        return {"error": str(e)}
    except Exception as e:
        logger.warning("MindBank API error: %s", e)
        return {"error": str(e)}


# Default namespace detection: uses cwd directory name as namespace.
# Users can override per-directory via mindbank.json "namespace" field,
# or add custom mappings by setting _PROJECT_NS in their environment.
#
# To add custom directory→namespace mappings, create a file at:
#   ~/.hermes/mindbank-namespaces.json
# with content like: {"my-project-dir": "myproject", "other-dir": "other"}
_PROJECT_NS: dict[str, str] = {}


def _load_project_ns_map():
    """Load custom directory→namespace mappings from config file."""
    global _PROJECT_NS
    try:
        hermes_home = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
        ns_file = os.path.join(hermes_home, "mindbank-namespaces.json")
        if os.path.exists(ns_file):
            with open(ns_file) as f:
                _PROJECT_NS.update(json.load(f))
    except Exception:
        pass


_load_project_ns_map()


def _detect_namespace() -> str:
    """Auto-detect namespace from current working directory.

    Priority:
    1. Custom mapping from mindbank-namespaces.json
    2. Parent directory name (if it looks like a project root)
    3. Current directory name (sanitized)
    4. 'hermes' fallback
    """
    try:
        cwd = os.getcwd()
        basename = os.path.basename(cwd).lower()
        parent = os.path.basename(os.path.dirname(cwd)).lower()

        # Check custom mapping first
        if basename in _PROJECT_NS:
            return _PROJECT_NS[basename]
        if parent in _PROJECT_NS:
            return _PROJECT_NS[parent]

        # Check parent mapping
        if parent in _PROJECT_NS:
            return _PROJECT_NS[parent]

        # Use directory name directly as namespace (sanitized)
        if basename and basename not in (".", "home", "root", "tmp"):
            # Sanitize: lowercase, replace non-alphanumeric with hyphens
            sanitized = "".join(c if c.isalnum() else "-" for c in basename).strip("-")
            if sanitized:
                return sanitized
    except Exception:
        pass
    return "hermes"



def _is_duplicate(api_url: str, label: str, namespace: str) -> bool:
    """Check if a node with similar label already exists."""
    if not label or len(label) < 10:
        return False
    try:
        result = _api_call(api_url, "POST", "/search/hybrid", {
            "query": label,
            "namespace": namespace,
            "limit": 3,
        }, timeout=3)
        if isinstance(result, list):
            for r in result:
                existing = r.get("label", "").lower().strip()
                if existing == label.lower().strip():
                    return True
        return False
    except Exception:
        return False

class MindBankProvider(MemoryProvider):
    """Graph-structured persistent memory for Hermes Agent."""

    def __init__(self):
        self._api_url = DEFAULT_API_URL
        self._namespace = ""  # project namespace for isolation
        self._session_id = ""
        self._sync_thread: Optional[threading.Thread] = None
        self._available = None  # cached availability check

    @property
    def name(self) -> str:
        return "mindbank"

    def is_available(self) -> bool:
        """Check if MindBank API is reachable. No heavy network calls."""
        if self._available is not None:
            return self._available
        # Quick health check with short timeout
        try:
            result = _api_call(self._api_url, "GET", "/health", timeout=3)
            self._available = result.get("status") == "ok"
        except Exception:
            self._available = False
        return self._available

    def save_config(self, values: dict, hermes_home: str) -> None:
        """Write config to $HERMES_HOME/mindbank.json."""
        from pathlib import Path
        config_path = Path(hermes_home) / "mindbank.json"
        config_path.write_text(json.dumps(values, indent=2))
    @staticmethod
    def config_fields() -> list:
        """Config fields for 'hermes memory setup'."""
        return [
            {
                "key": "api_url",
                "description": "MindBank API URL",
                "default": DEFAULT_API_URL,
                "required": False,
            },
            {
                "key": "namespace",
                "description": "Project namespace for memory isolation (auto-detected from cwd if empty)",
                "default": "",
                "required": False,
            },
        ]

    def post_setup(self, hermes_home: str, config: dict) -> None:
        """Called after setup wizard completes."""
        pass

    def initialize(self, session_id: str, **kwargs) -> None:
        """Initialize the provider."""
        self._session_id = session_id

        # Load config
        hermes_home = kwargs.get("hermes_home", "")
        if hermes_home:
            config_path = os.path.join(hermes_home, "mindbank.json")
            if os.path.exists(config_path):
                try:
                    with open(config_path) as f:
                        cfg = json.load(f)
                    self._api_url = cfg.get("api_url", DEFAULT_API_URL)
                    self._namespace = cfg.get("namespace", "")
                except Exception:
                    pass

        # Also check environment variable
        env_url = os.environ.get("MINDBANK_API_URL")
        if env_url:
            self._api_url = env_url

        # Auto-detect namespace from working directory if not set
        if not self._namespace:
            self._namespace = _detect_namespace()

        # Clean up stale MCP server processes (older than 1 hour)
        try:
            import subprocess
            import time
            result = subprocess.run(["pgrep", "-af", "mindbank-mcp"], capture_output=True, text=True, timeout=3)
            for line in result.stdout.strip().split("\n"):
                if not line.strip():
                    continue
                parts = line.split(None, 1)
                if len(parts) < 2:
                    continue
                try:
                    pid = int(parts[0])
                    # Don't kill our own parent process
                    if pid != os.getppid() and pid != os.getpid():
                        # Check if process is stale (running > 2 hours)
                        stat_file = f"/proc/{pid}/stat"
                        if os.path.exists(stat_file):
                            with open(stat_file) as f:
                                stat = f.read().split()
                            starttime = int(stat[21])
                            with open("/proc/uptime") as f:
                                uptime = float(f.read().split()[0])
                            age_seconds = uptime - (starttime / os.sysconf("SC_CLK_TCK"))
                            if age_seconds > 7200:  # 2 hours
                                os.kill(pid, 15)  # SIGTERM
                                logger.info("Cleaned up stale MCP process %d (age: %ds)", pid, int(age_seconds))
                except (ValueError, ProcessLookupError, PermissionError, OSError):
                    continue
        except Exception:
            pass

        logger.info("MindBank initialized: %s ns=%s (session: %s)", self._api_url, self._namespace or "*", session_id)

    def system_prompt_block(self) -> str:
        """Return context to inject into system prompt."""
        # Get snapshot (wake-up context) — filtered by namespace
        ns = self._namespace or "hermes"
        result = _api_call(self._api_url, "GET", f"/snapshot?namespace={ns}", timeout=5)
        if not result or "error" in result:
            return ""

        content = result.get("content", "")
        tokens = result.get("token_count", 0)
        if not content or content == "No memories stored yet.":
            return ""

        # Truncate if too large for system prompt
        max_chars = 3000
        if len(content) > max_chars:
            content = content[:max_chars] + "\n... [truncated]"

        return (
            f"## MindBank Memory ({tokens} tokens)\n"
            f"{content}\n\n"
            f"Use mindbank_search or mindbank_ask to recall more details."
        )

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        """Fetch relevant memories before each API call."""
        if not query or len(query) < 10:
            return ""

        # Use hybrid search for best recall — filtered by namespace
        ns = self._namespace or "hermes"
        body = {
            "query": query,
            "limit": 3,
            "workspace": "hermes",
            "namespace": ns,
        }
        result = _api_call(self._api_url, "POST", "/search/hybrid", body, timeout=15)

        if not result or "error" in result:
            return ""

        if isinstance(result, list) and len(result) > 0:
            lines = []
            for r in result[:3]:
                label = r.get("label", "")
                content = r.get("content", "")
                ntype = r.get("node_type", "")
                if content:
                    content = content[:150] + "..." if len(content) > 150 else content
                    lines.append(f"- [{ntype}] {label}: {content}")
            if lines:
                return "Relevant memories:\n" + "\n".join(lines)

        return ""

    def queue_prefetch(self, query: str, *, session_id: str = "") -> None:
        """Pre-warm for next turn (non-blocking)."""
        # Prefetch runs in background — the next turn will call prefetch()
        # with a fresh query, so we don't need to do anything here.
        pass

    def sync_turn(self, user_content: str, assistant_content: str, *, session_id: str = "") -> None:
        """Store conversation turn in MindBank (non-blocking)."""
        def _sync():
            try:
                # Skip very short messages
                label = user_content[:60].strip()
                if not label or len(user_content) < 30:
                    return
                # Detect node type from content — require 2+ keyword matches for noise reduction
                lower = user_content.lower()
                hits = 0
                node_type = "event"
                decision_kw = ["decided", "chose", "going with", "switching to", "switching"]
                problem_kw = ["bug", "broken", "error", "issue", "crash", "fails"]
                advice_kw = ["recommend", "suggest", "better to", "best practice"]
                pref_kw = ["prefer", "always use", "never use", "i like"]
                if sum(1 for w in decision_kw if w in lower) >= 1:
                    node_type = "decision"
                elif sum(1 for w in problem_kw if w in lower) >= 1:
                    node_type = "problem"
                elif sum(1 for w in advice_kw if w in lower) >= 2:
                    node_type = "advice"
                elif sum(1 for w in pref_kw if w in lower) >= 1:
                    node_type = "preference"
                else:
                    return  # Skip non-classifiable turns (reduces noise)

                # Dedup: skip if similar node already exists
                ns = self._namespace or "hermes"
                if _is_duplicate(self._api_url, label, ns):
                    return

                _api_call(self._api_url, "POST", "/nodes", {
                    "label": label,
                    "node_type": node_type,
                    "content": f"User: {user_content[:300]}\n\nAssistant: {assistant_content[:300]}",
                    "namespace": self._namespace or "hermes",
                    "summary": f"Session {session_id[:8]}",
                }, timeout=10)
            except Exception as e:
                logger.warning("MindBank sync failed: %s", e)

        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=5.0)
        self._sync_thread = threading.Thread(target=_sync, daemon=True)
        self._sync_thread.start()

    def on_session_end(self, messages: List[Dict[str, Any]]) -> None:
        """Extract memories from completed session."""
        if not messages:
            return

        def _extract():
            try:
                ns = self._namespace or "hermes"
                # Extract key turns from the conversation — only high-signal messages
                for msg in messages[-10:]:  # last 10 messages
                    role = msg.get("role", "")
                    content = msg.get("content", "")
                    if role != "user" or len(content) < 30:
                        continue
                    lower = content.lower()
                    # Only extract messages with strong decision/problem/fixed signals
                    node_type = None
                    if any(w in lower for w in ["decided", "chose", "going with", "switching to"]):
                        node_type = "decision"
                    elif any(w in lower for w in ["fixed", "solution", "workaround", "resolved"]):
                        node_type = "advice"
                    elif any(w in lower for w in ["bug", "broken", "error", "crash", "fails"]):
                        node_type = "problem"
                    elif any(w in lower for w in ["migrated", "upgraded", "deprecated", "deployed"]):
                        node_type = "fact"
                    if not node_type:
                        continue
                    label = content[:80].strip()
                    if _is_duplicate(self._api_url, label, ns):
                        continue
                    _api_call(self._api_url, "POST", "/nodes", {
                        "label": label,
                        "node_type": node_type,
                        "content": content[:500],
                        "namespace": ns,
                    }, timeout=10)
            except Exception as e:
                logger.warning("MindBank session extract failed: %s", e)

        thread = threading.Thread(target=_extract, daemon=True)
        thread.start()

    def on_memory_write(self, action: str, target: str, content: str) -> None:
        """Mirror built-in memory writes to MindBank."""
        if action in ("add", "replace") and content:
            def _write():
                try:
                    _api_call(self._api_url, "POST", "/nodes", {
                        "label": f"Memory: {target}",
                        "node_type": "preference" if target == "user" else "fact",
                        "content": content[:500],
                        "namespace": self._namespace or "hermes",
                        "summary": f"Built-in memory ({action})",
                    }, timeout=10)
                except Exception as e:
                    logger.warning("MindBank memory write mirror failed: %s", e)

            thread = threading.Thread(target=_write, daemon=True)
            thread.start()

    def get_tool_schemas(self) -> List[Dict[str, Any]]:
        """Return tool definitions for the agent."""
        return [STORE_SCHEMA, SEARCH_SCHEMA, ASK_SCHEMA, SNAPSHOT_SCHEMA, NEIGHBORS_SCHEMA]

    def handle_tool_call(self, tool_name: str, args: dict, **kwargs) -> str:
        """Handle tool calls from the agent."""
        if tool_name == "mindbank_store":
            return self._handle_store(args)
        elif tool_name == "mindbank_search":
            return self._handle_search(args)
        elif tool_name == "mindbank_ask":
            return self._handle_ask(args)
        elif tool_name == "mindbank_snapshot":
            return self._handle_snapshot(args)
        elif tool_name == "mindbank_neighbors":
            return self._handle_neighbors(args)
        else:
            return f"Unknown tool: {tool_name}"

    def _handle_store(self, args: dict) -> str:
        ns = args.get("namespace", "") or self._namespace or "hermes"
        node_type = args.get("type", "fact")
        label = args.get("label", "")
        content = args.get("content", "")

        # Store the node
        result = _api_call(self._api_url, "POST", "/nodes", {
            "label": label,
            "node_type": node_type,
            "content": content,
            "summary": args.get("summary", ""),
            "namespace": ns,
        }, timeout=10)

        if "error" in result:
            return f"Error storing memory: {result['error']}"

        node_id = result.get("id", "")

        # Create semantic edges to related nodes
        if node_id and content:
            try:
                self._create_semantic_edges(node_id, node_type, label, content, ns)
            except Exception:
                pass  # edges are best-effort

        return f"Stored: [{result.get('node_type')}] {result.get('label')} (id: {node_id[:8]})"

    def _create_semantic_edges(self, node_id: str, node_type: str, label: str, content: str, ns: str) -> None:
        """Create semantic edges between the new node and related existing nodes."""
        # Search for related nodes in the same namespace
        related = _api_call(self._api_url, "POST", "/search/hybrid", {
            "query": label + " " + content[:100],
            "namespace": ns,
            "limit": 5,
        }, timeout=10)

        if not isinstance(related, list) or len(related) == 0:
            return

        # Define edge type based on node type relationships
        edge_rules = {
            "decision": {"default": "relates_to", "project": "decided_by", "problem": "contradicts"},
            "problem": {"default": "relates_to", "decision": "contradicts", "advice": "supports"},
            "advice": {"default": "relates_to", "decision": "supports", "problem": "supports"},
            "fact": {"default": "relates_to"},
            "preference": {"default": "relates_to"},
        }
        rules = edge_rules.get(node_type, {"default": "relates_to"})

        edges_to_create = []
        for rel in related[:3]:  # max 3 edges per store
            rel_id = rel.get("node_id", "")
            rel_type = rel.get("node_type", "fact")
            if rel_id and rel_id != node_id:
                edge_type = rules.get(rel_type, rules["default"])
                edges_to_create.append({
                    "source_id": node_id,
                    "target_id": rel_id,
                    "edge_type": edge_type,
                })

        if edges_to_create:
            _api_call(self._api_url, "POST", "/edges/batch", {"edges": edges_to_create}, timeout=10)

    def _handle_search(self, args: dict) -> str:
        ns = args.get("namespace", "") or self._namespace or "hermes"
        result = _api_call(self._api_url, "POST", "/search/hybrid", {
            "query": args.get("query", ""),
            "namespace": ns,
            "limit": args.get("limit", 5),
        }, timeout=15)

        if "error" in result:
            return f"Search error: {result['error']}"

        if not isinstance(result, list) or len(result) == 0:
            return "No memories found."

        lines = []
        for r in result:
            label = r.get("label", "")
            ntype = r.get("node_type", "")
            content = r.get("content", "")
            content = content[:120] + "..." if len(content) > 120 else content
            lines.append(f"- [{ntype}] {label}: {content}")

        return "\n".join(lines)

    def _handle_ask(self, args: dict) -> str:
        body = {
            "query": args.get("query", ""),
            "max_tokens": 1000,
        }
        ns = args.get("namespace", "") or self._namespace or "hermes"
        body["namespace"] = ns
        result = _api_call(self._api_url, "POST", "/ask", body, timeout=15)

        if "error" in result:
            return f"Ask error: {result['error']}"

        context = result.get("context", "")
        if not context:
            return "No relevant memories found."

        return context

    def _handle_neighbors(self, args: dict) -> str:
        node_id = args.get("node_id", "")
        if not node_id:
            return "Error: node_id is required"
        depth = min(args.get("depth", 1), 3)
        result = _api_call(self._api_url, "GET", f"/nodes/{node_id}/neighbors?depth={depth}&limit=20", timeout=10)

        if "error" in result:
            return f"Neighbors error: {result['error']}"

        if not isinstance(result, list) or len(result) == 0:
            return "No connections found for this node."

        lines = []
        for nb in result[:10]:
            etype = nb.get("edge_type", "related")
            ntype = nb.get("node_type", "fact")
            label = nb.get("label", "?")
            content = (nb.get("content") or "")[:80]
            d = nb.get("depth", 1)
            prefix = "  " * (d - 1)
            lines.append(f"{prefix}- [{etype}] [{ntype}] {label}: {content}")

        return f"Connections ({len(result)} total):\n" + "\n".join(lines)

    def _handle_snapshot(self, args: dict) -> str:
        ns = args.get("namespace", "") or self._namespace or "hermes"
        path = f"/snapshot?namespace={ns}"
        result = _api_call(self._api_url, "GET", path, timeout=5)

        if "error" in result:
            return f"Snapshot error: {result['error']}"

        content = result.get("content", "")
        tokens = result.get("token_count", 0)
        if not content:
            return "No snapshot available."

        return f"{content}\n\n({tokens} tokens)"

    def shutdown(self) -> None:
        """Clean up."""
        if self._sync_thread and self._sync_thread.is_alive():
            self._sync_thread.join(timeout=2.0)
        logger.info("MindBank provider shut down")


def register(ctx) -> None:
    """Entry point — called by the memory plugin discovery system."""
    ctx.register_memory_provider(MindBankProvider())
