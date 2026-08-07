#!/usr/bin/env python3
"""Resolve a Hermes session id to its project namespace.

Reads Hermes' state.db (session system prompt + message transcript) and
frequency-scores /home/<user>/<project> mentions, weighting later messages more
so a mid-session project switch takes over. Prints the namespace (or nothing).

Usage: hermes-session-ns.py <session_id>
"""
import re
import sqlite3
import sys
from collections import Counter
from pathlib import Path

# Directories that appear in transcripts but are not projects.
GENERIC = {
    "rat", "home", "hermes", "wsl", "mnt", "tmp", "root", "usr", "bin",
    "lib", "etc", "opt", "var", "cache", "config", "nvm", "local",
    "node_modules", "site-packages", "dist", "build", "target",
}


def db_paths():
    home = Path.home() / ".hermes"
    yield home / "state.db"
    for p in sorted((home / "profiles").glob("*/state.db")):
        yield p


def transcript(session_id):
    """Return (system_prompt, message_texts) for a session from any state.db."""
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
            cur.execute("SELECT system_prompt FROM sessions WHERE id = ?", (session_id,))
            row = cur.fetchone()
            cur.execute(
                "SELECT content FROM messages WHERE session_id = ? "
                "AND content IS NOT NULL AND length(content) > 40 "
                "ORDER BY id DESC LIMIT 150",
                (session_id,),
            )
            texts = [r[0] for r in cur.fetchall()][::-1]  # chronological
            sp = row[0] if row and row[0] else ""
        except Exception:
            con.close()
            continue
        con.close()
        if texts or sp:
            return sp, texts
    return "", []


# Linux: /home/<user>/<proj> ; WSL: \wsl.localhost\...\home\rat\<proj> ; Windows: C:\Users\...\<proj>
PATH_RE = re.compile(
    r"(?:" + re.escape(str(Path.home())) + r"/|\\wsl[^\]*\\home\\rat\\|/mnt/[a-zA-Z]/[^\s/]+/|"
    r"[a-zA-Z]:\\Users\\[^\\]+\\)([A-Za-z0-9_.\-]+)"
)


def projects_from(text):
    out = []
    for m in PATH_RE.findall(text):
        name = m.rstrip("/")
        if not name or name.startswith(".") or name.lower() in GENERIC:
            continue
        out.append(name.lower())
    return out


def main():
    if len(sys.argv) < 2:
        return
    session_id = sys.argv[1].strip()
    if not session_id:
        return
    sp, texts = transcript(session_id)
    if not texts and not sp:
        return
    if sp:
        texts = [sp] + texts
    counts = Counter()
    n = float(len(texts))
    for i, t in enumerate(texts):
        for proj in projects_from(t):
            weight = 10 + int(i / n * 10)  # later messages weigh more
            counts[proj] += weight
    if not counts:
        return
    best = counts.most_common(1)[0][0]
    sys.stdout.write(best)


if __name__ == "__main__":
    main()
