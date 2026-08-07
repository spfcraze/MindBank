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
import re
import threading
import time
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


# Generic buckets that are not real projects: recall from a session in one of
# these should span the whole graph, not isolate to the (near-empty, noisy)
# bucket itself. Real project namespaces (klixsor, mindbank, …) stay isolated.
_GENERIC_NS = {"", "hermes", "global", "default"}


def _is_generic_ns(ns: str) -> bool:
    return (ns or "").lower() in _GENERIC_NS


def _search_nodes(result) -> list:
    """Normalize a /search/hybrid response into a list of node dicts.

    The endpoint returns a token-budgeted object {"nodes":[...], ...}, but
    earlier revisions returned a bare list. Accept both (and error/empty dicts)
    so every search consumer sees a plain list. Missing this normalization made
    every isinstance(result, list) check fail, silently returning "no results".
    """
    if isinstance(result, list):
        return result
    if isinstance(result, dict) and isinstance(result.get("nodes"), list):
        return result["nodes"]
    return []


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
    2. Working directory path patterns (e.g., /home/<user>/mindbank -> mindbank)
    3. Parent directory name (if it looks like a project root)
    4. Current directory name (sanitized)
    5. 'hermes' fallback
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

        # Extract project from working directory path
        # Patterns: /home/<user>/projectname, ~/projectname, /workspace/projectname
        wd_patterns = [
            r'/home/[^/\s]+/([a-zA-Z0-9_-]+)',
            r'~/([a-zA-Z0-9_-]+)',
            r'/workspace/([a-zA-Z0-9_-]+)',
            r'/mnt/[^/]+/Users/[^/]+/([a-zA-Z0-9_-]+)',
        ]
        for pattern in wd_patterns:
            matches = re.findall(pattern, cwd)
            for match in matches:
                # Filter out common non-project directories
                if match not in ('.hermes', '.claude', '.config', '.local', '.ssh', 
                                  '.git', '.github', '.vscode', 'node_modules', 
                                  'vendor', 'dist', 'build', 'tmp', 'temp', 
                                  'rat', 'user', 'home', 'root'):
                    return match.lower()

        # Check parent mapping
        if parent in _PROJECT_NS:
            return _PROJECT_NS[parent]

        # Use directory name directly as namespace (sanitized)
        if basename and basename not in (".", "home", "root", "tmp", "rat", "user"):
            # Sanitize: lowercase, replace non-alphanumeric with hyphens
            sanitized = "".join(c if c.isalnum() else "-" for c in basename).strip("-")
            if sanitized:
                return sanitized
    except Exception:
        pass
    return "hermes"


def _detect_profile() -> str:
    """Detect current Hermes profile from environment or config."""
    # Check environment variable first
    profile = os.environ.get("HERMES_PROFILE", "")
    if profile:
        return profile
    
    # Check HERMES_HOME for profile subfolder
    hermes_home = os.environ.get("HERMES_HOME", os.path.expanduser("~/.hermes"))
    try:
        # If we're in a profile-specific sessions dir, extract profile name
        cwd = os.getcwd()
        if "/profiles/" in cwd:
            parts = cwd.split("/profiles/")
            if len(parts) > 1:
                profile = parts[1].split("/")[0]
                if profile and profile not in (".", ".."):
                    return profile
    except Exception:
        pass
    
    return "default"
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
        for r in _search_nodes(result):
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
        self._profile = "default"  # hermes profile for multi-user separation
        self._session_id = ""
        self._sync_thread: Optional[threading.Thread] = None
        self._available = None  # cached availability check
        self._available_ts = 0.0  # when availability was last probed
        # Client-side keyword capture (sync_turn / on_session_end). Off by
        # default: the server now does higher-quality LLM session mining, and
        # these keyword heuristics produce the noisy "regex-era" nodes. Opt in
        # via mindbank.json "capture_turns": true or MINDBANK_CAPTURE_TURNS=1.
        self._capture_turns = False

    @property
    def name(self) -> str:
        return "mindbank"

    def is_available(self) -> bool:
        """Check if MindBank API is reachable. No heavy network calls.

        Result is cached with a short TTL so a transient outage at startup
        doesn't disable memory for the entire session (previously the first
        failure was cached permanently).
        """
        now = time.monotonic()
        # Cache successes for 60s, failures for only 10s so recovery is quick.
        ttl = 60.0 if self._available else 10.0
        if self._available is not None and (now - self._available_ts) < ttl:
            return self._available
        try:
            result = _api_call(self._api_url, "GET", "/health", timeout=3)
            self._available = result.get("status") == "ok"
        except Exception:
            self._available = False
        self._available_ts = now
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
                    self._capture_turns = bool(cfg.get("capture_turns", False))
                except Exception:
                    pass

        # Also check environment variable
        env_url = os.environ.get("MINDBANK_API_URL")
        if env_url:
            self._api_url = env_url
        if os.environ.get("MINDBANK_CAPTURE_TURNS", "").lower() in ("1", "true", "yes"):
            self._capture_turns = True

        # Auto-detect namespace from working directory if not set
        if not self._namespace:
            self._namespace = _detect_namespace()

        # Auto-detect profile
        self._profile = _detect_profile()

        logger.info("MindBank initialized: %s ns=%s profile=%s (session: %s)", self._api_url, self._namespace or "*", self._profile, session_id)
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

    def system_prompt_block(self) -> str:
        """Return context to inject into system prompt."""
        # Wake-up snapshot. Generic buckets (hermes/global) draw from the whole
        # graph; real project namespaces stay scoped but broaden if empty, so
        # the wake-up context is never blank when knowledge exists elsewhere.
        ns = self._namespace or "hermes"
        filt = "" if _is_generic_ns(ns) else ns
        # The global (no-namespace) snapshot is a precomputed blob that nothing
        # rebuilds on a schedule, so it goes stale. regenerate=1 forces a fresh
        # top-node query (~100ms, once per session) and refreshes the shared
        # blob. Namespace-scoped snapshots already regenerate on cache miss.
        if filt:
            path = f"/snapshot?namespace={filt}"
        else:
            path = "/snapshot?regenerate=1"
        result = _api_call(self._api_url, "GET", path, timeout=8)
        content = result.get("content", "") if isinstance(result, dict) else ""
        tokens = result.get("token_count", 0) if isinstance(result, dict) else 0
        if (not content or content == "No memories stored yet.") and filt:
            # Project namespace empty — broaden to a fresh global snapshot.
            result = _api_call(self._api_url, "GET", "/snapshot?regenerate=1", timeout=8)
            content = result.get("content", "") if isinstance(result, dict) else ""
            tokens = result.get("token_count", 0) if isinstance(result, dict) else 0
        if not content or content == "No memories stored yet.":
            return ""

        # Truncate if too large for system prompt
        max_chars = 3000
        if len(content) > max_chars:
            content = content[:max_chars] + "\n... [truncated]"

        block = (
            f"## MindBank Memory ({tokens} tokens)\n"
            f"{content}\n\n"
            f"Use mindbank_search or mindbank_ask to recall more details."
        )

        # Add gaps analysis (what's missing)
        try:
            gaps = _api_call(self._api_url, "GET", f"/analyze/gaps?namespace={ns}", timeout=5)
            if gaps and isinstance(gaps, dict) and gaps.get("count", 0) > 0:
                summary = gaps.get("summary", {})
                gap_parts = []
                if summary.get("unsolved_problem"):
                    gap_parts.append(f"{summary['unsolved_problem']} unsolved problems")
                if summary.get("unanswered_question"):
                    gap_parts.append(f"{summary['unanswered_question']} unanswered questions")
                if summary.get("orphan"):
                    gap_parts.append(f"{summary['orphan']} orphan memories")
                if summary.get("stale"):
                    gap_parts.append(f"{summary['stale']} stale memories")
                if gap_parts:
                    block += f"\n\n## Knowledge Gaps: {', '.join(gap_parts)}. Consider addressing these."
        except Exception:
            pass

        # Add diff (what changed since last session)
        try:
            diff = _api_call(self._api_url, "GET", f"/analyze/diff?since=last-session", timeout=5)
            if diff and isinstance(diff, dict):
                s = diff.get("summary", {})
                if any(v > 0 for v in s.values()):
                    changes = []
                    if s.get("new_nodes"):
                        changes.append(f"{s['new_nodes']} new memories")
                    if s.get("updated_nodes"):
                        changes.append(f"{s['updated_nodes']} updated")
                    if s.get("deleted_nodes"):
                        changes.append(f"{s['deleted_nodes']} deleted")
                    if changes:
                        block += f"\n\n## Recent Changes: {', '.join(changes)} since last session."
        except Exception:
            pass

        return block

    def _hybrid_search(self, query: str, ns: str, limit: int) -> list:
        """Hybrid search with namespace scoping. Returns a list of node dicts.

        Generic buckets (hermes/global) search the whole graph. Real project
        namespaces stay isolated, but broaden to the whole graph if they yield
        nothing — so recall is never empty when knowledge exists elsewhere,
        without diluting a populated project namespace.
        """
        filt = "" if _is_generic_ns(ns) else ns
        result = _api_call(self._api_url, "POST", "/search/hybrid", {
            "query": query, "limit": limit, "namespace": filt,
        }, timeout=15)
        nodes = _search_nodes(result)
        if not nodes and filt:
            result = _api_call(self._api_url, "POST", "/search/hybrid", {
                "query": query, "limit": limit,
            }, timeout=15)
            nodes = _search_nodes(result)
        return nodes

    def prefetch(self, query: str, *, session_id: str = "") -> str:
        """Fetch relevant memories before each API call."""
        if not query or len(query) < 10:
            return ""

        ns = self._namespace or "hermes"
        nodes = self._hybrid_search(query, ns, 3)
        if nodes:
            lines = []
            for r in nodes[:3]:
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
        """Store conversation turn in MindBank (non-blocking).

        Disabled unless capture_turns is set — server-side LLM session mining
        produces higher-quality memories than this keyword heuristic.
        """
        if not self._capture_turns:
            return

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
                    "metadata": {"profile": self._profile},
                }, timeout=10)
            except Exception as e:
                logger.warning("MindBank sync failed: %s", e)

        # Don't block the agent turn waiting on a prior sync — if one is still
        # running, skip this turn's capture rather than stalling up to 5s.
        if self._sync_thread and self._sync_thread.is_alive():
            return
        self._sync_thread = threading.Thread(target=_sync, daemon=True)
        self._sync_thread.start()

    def on_session_end(self, messages: List[Dict[str, Any]]) -> None:
        """Extract memories from completed session.

        Disabled unless capture_turns is set — the server's session miner does
        this with an LLM instead of keyword matching. See sync_turn.
        """
        if not messages or not self._capture_turns:
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
                        "metadata": {"profile": self._profile},
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
                        "metadata": {"profile": self._profile},
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

        # Check for contradictions before storing decisions
        contradiction_warning = ""
        if node_type in ("decision", "advice") and label:
            try:
                conflicts = _api_call(self._api_url, "GET", f"/analyze/contradictions?namespace={ns}", timeout=5)
                if conflicts and isinstance(conflicts, dict) and conflicts.get("contradictions"):
                    for c in conflicts["contradictions"][:5]:
                        if (label.lower() in c.get("source_label", "").lower() or
                            label.lower() in c.get("target_label", "").lower() or
                            c.get("source_label", "").lower() in label.lower() or
                            c.get("target_label", "").lower() in label.lower()):
                            contradiction_warning = (
                                f"\nWARNING: This may contradict existing memory: "
                                f"\"{c.get('source_label', '')}\" vs \"{c.get('target_label', '')}\""
                            )
                            break
            except Exception:
                pass

        # Store the node
        result = _api_call(self._api_url, "POST", "/nodes", {
            "label": label,
            "node_type": node_type,
            "content": content,
            "summary": args.get("summary", ""),
            "namespace": ns,
            "metadata": {"profile": self._profile},
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

        response = f"Stored: [{result.get('node_type')}] {result.get('label')} (id: {node_id[:8]})"
        if contradiction_warning:
            response += contradiction_warning
        return response

    def _create_semantic_edges(self, node_id: str, node_type: str, label: str, content: str, ns: str) -> None:
        """Create semantic edges between the new node and related existing nodes."""
        # Search for related nodes in the same namespace
        related = _api_call(self._api_url, "POST", "/search/hybrid", {
            "query": label + " " + content[:100],
            "namespace": ns,
            "limit": 5,
        }, timeout=10)

        related = _search_nodes(related)
        if not related:
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
        nodes = self._hybrid_search(args.get("query", ""), ns, args.get("limit", 5))
        if not nodes:
            return "No memories found."

        lines = []
        for r in nodes:
            label = r.get("label", "")
            ntype = r.get("node_type", "")
            content = r.get("content", "")
            content = content[:120] + "..." if len(content) > 120 else content

            # Confidence is included inline by /search/hybrid when available.
            # (Previously this issued a serial GET /analyze/confidence per
            # result — an N+1 that added up to ~3s*N of latency to every
            # search on the agent's synchronous path.)
            confidence_tag = ""
            level = r.get("trust_level", "")
            if level:
                score = r.get("confidence", 0) or 0
                confidence_tag = f" [{level}:{score:.2f}]"

            lines.append(f"- [{ntype}]{confidence_tag} {label}: {content}")

        return "\n".join(lines)

    def _handle_ask(self, args: dict) -> str:
        query = args.get("query", "")
        ns = args.get("namespace", "") or self._namespace or "hermes"
        filt = "" if _is_generic_ns(ns) else ns

        def _ask(namespace):
            body = {"query": query, "max_tokens": 1000}
            if namespace:
                body["namespace"] = namespace
            return _api_call(self._api_url, "POST", "/ask", body, timeout=15)

        result = _ask(filt)
        if isinstance(result, dict) and "error" in result:
            return f"Ask error: {result['error']}"

        context = result.get("context", "") if isinstance(result, dict) else ""
        if not context and filt:
            # Broaden to the whole graph when this project namespace has no answer.
            result = _ask("")
            context = result.get("context", "") if isinstance(result, dict) else ""
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
