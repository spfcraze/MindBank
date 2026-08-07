#!/usr/bin/env python3
"""Raw session activity (MindBank-independent): what sessions actually DID.

Reads Hermes state.db tool-call events directly — no MindBank curation or
summarization involved. Reports per-tool and per-session event counts plus the
number of raw memory-write tool calls (the "intent to persist" signal).

Usage: hermes-raw-events.py [window_seconds]
"""
import json
import re
import sqlite3
import sys
import time
from collections import Counter, defaultdict
from pathlib import Path

GENERIC = {
    "rat", "home", "hermes", "wsl", "mnt", "tmp", "root", "usr", "bin",
    "lib", "etc", "opt", "var", "cache", "config", "nvm", "local",
    "node_modules", "site-packages", "dist", "build", "target",
}
PROJECT_RE = re.compile(
    r"(?:" + re.escape(str(Path.home())) + r"/|\\\\wsl[^\\\\]*\\\\home\\\\rat\\\\|"
    r"/mnt/[a-zA-Z]/[^\s/]+/|"
    r"[a-zA-Z]:\\\\Users\\\\[^\\\\]+\\\\)([A-Za-z0-9_.\-]+)"
)
# tools that signal an intent to persist knowledge into MindBank
MEMORY_TOOLS = {"memory", "mindbank_search", "mindbank_ask", "mindbank_save",
                "mindbank_create_node", "mindbank", "create_node", "create_nodes"}


def db_paths():
    home = Path.home() / ".hermes"
    yield home / "state.db"
    for p in sorted((home / "profiles").glob("*/state.db")):
        yield p


def main():
    window = 3600
    if len(sys.argv) > 1:
        try:
            window = max(60, min(int(sys.argv[1]), 86400))
        except ValueError:
            pass
    now = time.time()
    cutoff = now - window

    tool_counts = Counter()          # tool -> count
    tool_sessions = defaultdict(set)  # tool -> {sid}
    sess_events = Counter()          # sid -> total events
    sess_tools = defaultdict(Counter)  # sid -> {tool: count}
    memory_calls = 0
    active_sessions = set()

    # capture text for namespace derivation for the most active sessions
    sess_text = defaultdict(list)

    for db in db_paths():
        if not db.exists():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
            con.execute("PRAGMA query_only = ON")
            cur = con.cursor()
            cur.execute(
                "SELECT session_id, tool_name, content, timestamp FROM messages "
                "WHERE timestamp > ? AND tool_name IS NOT NULL AND tool_name != '' "
                "ORDER BY timestamp DESC LIMIT 8000",
                (cutoff,),
            )
            rows = cur.fetchall()
            con.close()
        except Exception:
            continue
        for sid, tool, content, ts in rows:
            tool = tool.strip()
            active_sessions.add(sid)
            tool_counts[tool] += 1
            tool_sessions[tool].add(sid)
            sess_events[sid] += 1
            sess_tools[sid][tool] += 1
            if tool in MEMORY_TOOLS:
                memory_calls += 1
            if content:
                sess_text[sid].append(content[:400])

    # namespace for the most active sessions (top 15 by events)
    top_sids = [sid for sid, _ in sess_events.most_common(15)]
    ns_for = {}
    for db in db_paths():
        if not db.exists():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
            con.execute("PRAGMA query_only = ON")
            cur = con.cursor()
        except Exception:
            continue
        for sid in top_sids:
            if sid in ns_for:
                continue
            try:
                cur.execute(
                    "SELECT content FROM messages WHERE session_id = ? "
                    "AND content IS NOT NULL AND length(content) > 30 "
                    "ORDER BY id DESC LIMIT 40", (sid,))
                texts = [r[0] for r in cur.fetchall()][::-1]
            except Exception:
                continue
            counts = Counter()
            for t in texts:
                for m in PROJECT_RE.findall(t):
                    name = m.rstrip("/").lower()
                    if name and not name.startswith(".") and name not in GENERIC:
                        counts[name] += 1
            if counts:
                ns_for[sid] = counts.most_common(1)[0][0]
        con.close()

    tool_stats = [{"tool": t, "count": c, "sessions": len(tool_sessions[t])}
                  for t, c in tool_counts.most_common(20)]
    sessions = []
    for sid, total in sess_events.most_common(20):
        sessions.append({
            "id": sid,
            "events": total,
            "namespace": ns_for.get(sid, ""),
            "tools": [{"tool": t, "count": c} for t, c in sess_tools[sid].most_common(6)],
        })

    sys.stdout.write(json.dumps({
        "window": window,
        "now": now,
        "active_sessions": len(active_sessions),
        "sessions_with_events": len(sess_events),
        "total_events": sum(tool_counts.values()),
        "memory_write_calls": memory_calls,
        "tool_stats": tool_stats,
        "sessions": sessions,
    }))


if __name__ == "__main__":
    main()
