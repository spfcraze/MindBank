#!/usr/bin/env python3
"""Fleet status for the JSPACE tab: active Hermes sessions from state.db.

Outputs JSON (best-effort; empty object on failure):
{
  "active_total": 561, "active_24h": 35, "busy_last_hour": 1,
  "sessions": [ {"id": "...", "messages": 847, "last_activity": 1786050000,
                 "started_at": 1786050000, "namespace": "pr-pilot"} ]
}
Namespace is derived only for the most recently active sessions (top 20) to
keep this fast; older sessions report namespace "".
"""
import json
import re
import sqlite3
import sys
import time
from collections import Counter
from pathlib import Path

GENERIC = {
    "rat", "home", "hermes", "wsl", "mnt", "tmp", "root", "usr", "bin",
    "lib", "etc", "opt", "var", "cache", "config", "nvm", "local",
    "node_modules", "site-packages", "dist", "build", "target",
}
PATH_RE = re.compile(
    r"(?:" + re.escape(str(Path.home())) + r"/|\\\\wsl[^\\\\]*\\\\home\\\\rat\\\\|"
    r"/mnt/[a-zA-Z]/[^\s/]+/|"
    r"[a-zA-Z]:\\\\Users\\\\[^\\\\]+\\\\)([A-Za-z0-9_.\-]+)"
)


def db_paths():
    home = Path.home() / ".hermes"
    yield home / "state.db"
    for p in sorted((home / "profiles").glob("*/state.db")):
        yield p


def find_session_rows(session_ids):
    """Return {sid: (system_prompt, message_texts)} for the ids present."""
    out = {}
    for db in db_paths():
        if not db.exists():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
            con.execute("PRAGMA query_only = ON")
        except Exception:
            continue
        try:
            cur = con.cursor()
            for sid in session_ids:
                if sid in out:
                    continue
                cur.execute("SELECT system_prompt FROM sessions WHERE id = ?", (sid,))
                row = cur.fetchone()
                cur.execute(
                    "SELECT content FROM messages WHERE session_id = ? "
                    "AND content IS NOT NULL AND length(content) > 40 "
                    "ORDER BY id DESC LIMIT 80",
                    (sid,),
                )
                texts = [r[0] for r in cur.fetchall()][::-1]
                sp = row[0] if row and row[0] else ""
                if texts or sp:
                    out[sid] = (sp, texts)
        except Exception:
            pass
        con.close()
    return out


def namespace_for(sp, texts):
    if sp:
        texts = [sp] + texts
    counts = Counter()
    n = float(len(texts))
    for i, t in enumerate(texts):
        for m in PATH_RE.findall(t):
            name = m.rstrip("/")
            if not name or name.startswith(".") or name.lower() in GENERIC:
                continue
            counts[name.lower()] += 10 + int(i / n * 10)
    if not counts:
        return ""
    return counts.most_common(1)[0][0]


def main():
    now = time.time()
    result = {"active_total": 0, "active_24h": 0, "busy_last_hour": 0, "sessions": []}
    for db in db_paths():
        if not db.exists():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
            con.execute("PRAGMA query_only = ON")
            cur = con.cursor()
        except Exception:
            continue
        try:
            cur.execute(
                "SELECT s.id, "
                "(SELECT COUNT(*) FROM messages m WHERE m.session_id = s.id), "
                "(SELECT MAX(m.timestamp) FROM messages m WHERE m.session_id = s.id), "
                "s.started_at "
                "FROM sessions s WHERE s.ended_at IS NULL"
            )
            rows = cur.fetchall()
        except Exception:
            con.close()
            continue
        con.close()
        for sid, msgs, last_ts, started in rows:
            last_ts = last_ts or 0
            result["active_total"] += 1
            if started and started > now - 86400:
                result["active_24h"] += 1
            if last_ts and last_ts > now - 3600:
                result["busy_last_hour"] += 1
            result["sessions"].append(
                {"id": sid, "messages": msgs, "last_activity": last_ts,
                 "started_at": started or 0, "namespace": ""}
            )
    # Namespace for the most recently active (top 20)
    result["sessions"].sort(key=lambda s: s["last_activity"], reverse=True)
    top = [s for s in result["sessions"][:20]]
    transcripts = find_session_rows([s["id"] for s in top])
    for s in top:
        if s["id"] in transcripts:
            sp, texts = transcripts[s["id"]]
            s["namespace"] = namespace_for(sp, texts)
    sys.stdout.write(json.dumps(result))


if __name__ == "__main__":
    main()
