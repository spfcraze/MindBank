#!/usr/bin/env python3
"""Live J-space: what the fleet of Hermes sessions is reasoning about NOW.

Reads recent messages (incl. the model's internal `reasoning` traces) from all
Hermes state.dbs read-only and reports:
- per-session latest activity (role, tool, reasoning/content snippets, age)
- live project distribution (namespaces mentioned in recent reasoning/work)
- top terms appearing in recent reasoning (the fleet's "current thoughts")

Usage: hermes-live-reasoning.py [window_seconds]
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
PROJECT_RE = re.compile(
    r"(?:" + re.escape(str(Path.home())) + r"/|\\\\wsl[^\\\\]*\\\\home\\\\rat\\\\|"
    r"/mnt/[a-zA-Z]/[^\s/]+/|"
    r"[a-zA-Z]:\\\\Users\\\\[^\\\\]+\\\\)([A-Za-z0-9_.\-]+)"
)
STOP = set("""the a an and or but if then else with for from that this is are was were
be been being to of in on at by as it its they their we our you your i my me
not no so just like about would could should may might will shall can should
into out over under after before when while where who whom which what why how
do does did done have has had having more most less least very really quite
there here these those also then than once both each other another any some all
one two first second third now """.split())


def db_paths():
    home = Path.home() / ".hermes"
    yield home / "state.db"
    for p in sorted((home / "profiles").glob("*/state.db")):
        yield p


def main():
    window = 600
    if len(sys.argv) > 1:
        try:
            window = max(30, min(int(sys.argv[1]), 3600))
        except ValueError:
            pass
    now = time.time()
    cutoff = now - window
    messages_scanned = 0
    memory_ops = []  # MindBank memory tool activity (state.db)

    # session_id -> latest message dict
    latest = {}
    # per-session: all recent text (for namespace derivation), plus totals
    sess_text = {}
    project_counts = Counter()
    reason_terms = Counter()
    sess_terms = {}          # sid -> {term: weighted_count}
    term_sessions = {}       # term -> Counter(sid)
    reason_total_chars = 0

    for db in db_paths():
        if not db.exists():
            continue
        try:
            con = sqlite3.connect(f"file:{db}?mode=ro", uri=True, timeout=5)
            con.execute("PRAGMA query_only = ON")
            cur = con.cursor()
            cur.execute(
                "SELECT session_id, role, content, reasoning, reasoning_content, "
                "       codex_reasoning_items, tool_name, timestamp "
                "FROM messages WHERE timestamp > ? ORDER BY timestamp DESC LIMIT 4000",
                (cutoff,),
            )
            rows = cur.fetchall()
            con.close()
        except Exception:
            continue
        for sid, role, content, reasoning, reasoning_content, codex_items, tool, ts in rows:
            messages_scanned += 1
            # MindBank memory activity: sessions using MindBank as their memory
            # layer (MCP tools / plugin) may emit little or no reasoning — their
            # memory calls ARE the live signal.
            if tool and (tool.startswith("mcp_mindbank_") or tool.startswith("mindbank_") or tool == "memory"):
                snippet = (content or "")
                snippet = re.sub(r"<untrusted_tool_result[^>]*>|</untrusted_tool_result>", "", snippet)
                snippet = re.sub(r"\s+", " ", snippet).strip()[:110]
                memory_ops.append({
                    "id": sid, "tool": tool, "snippet": snippet,
                    "ts": ts, "age": int(now - ts),
                })
            # reasoning may live in either column depending on provider
            reas = reasoning or reasoning_content or ""
            if codex_items:
                try:
                    if isinstance(codex_items, str):
                        reas = reas or codex_items[:600]
                except Exception:
                    pass
            if sid not in latest or ts > latest[sid]["ts"]:
                latest[sid] = {
                    "id": sid, "role": role or "",
                    "content": (content or "")[:160],
                    "reasoning": reas[:240],
                    "tool": tool or "",
                    "ts": ts, "age": int(now - ts),
                }
            # also remember the most recent message that HAS reasoning, so a
            # session mid-tool-loop still shows its latest thinking
            if len(reas.strip()) > 20:
                if "reasoning_ts" not in latest[sid] or ts > latest[sid]["reasoning_ts"]:
                    latest[sid]["reasoning"] = reas[:240]
                    latest[sid]["reasoning_ts"] = ts
                    latest[sid]["reasoning_age"] = int(now - ts)
            text = ""
            if reasoning:
                text += reasoning[:600]
            if content:
                text += " " + content[:300]
            if not text:
                continue
            sess_text.setdefault(sid, []).append(text)
            reason_total_chars += min(len(reasoning or ""), 600)
            for m in PROJECT_RE.findall(text):
                name = m.rstrip("/").lower()
                if name and not name.startswith(".") and name not in GENERIC:
                    project_counts[name] += 1
            # Recency weight: a term from a fresh message counts more than the
            # same term from the start of the window (J-thought coefficient).
            recency = max(0.2, 1.0 - (now - ts) / float(window))
            # DCG-style rank weight (JADR §4.3): a term first mentioned early in
            # the reasoning is more salient than one mentioned late, weighted
            # 1/log2(rank+1) by the order of its first mention.
            words = [w.lower() for w in re.findall(r"[A-Za-z][A-Za-z\-]{4,}", reas[:600])
                     if w.lower() not in STOP and not w.lower().endswith("-")]
            seen_rank = {}
            for rank, w in enumerate(words, start=1):
                if w not in seen_rank:
                    seen_rank[w] = rank
            for w, rank in seen_rank.items():
                dcg = 1.0 / (__import__("math").log2(rank + 1))
                reason_terms[w] += recency * dcg
                st = sess_terms.setdefault(sid, {})
                st[w] = st.get(w, 0.0) + recency * dcg
                ts2 = term_sessions.setdefault(w, Counter())
                ts2[sid] += 1

    # namespace per active session from its recent text
    for sid, texts in sess_text.items():
        if sid not in latest:
            continue
        counts = Counter()
        for t in texts:
            for m in PROJECT_RE.findall(t):
                name = m.rstrip("/").lower()
                if name and not name.startswith(".") and name not in GENERIC:
                    counts[name] += 1
        latest[sid]["namespace"] = counts.most_common(1)[0][0] if counts else ""

    sessions = sorted(latest.values(), key=lambda s: s["age"])[:40]
    projects = [{"name": n, "count": c} for n, c in project_counts.most_common(12)]

    # J-thought: sparse, coefficient-weighted concept state (J-CoT).
    top_terms = [t for t, _ in reason_terms.most_common(14) if reason_terms[t] >= 1.0]
    if top_terms:
        topw = max(reason_terms[t] for t in top_terms)
        jthought = [
            {"term": t, "coefficient": round(reason_terms[t] / topw, 3),
             "sessions": [{"id": s, "count": c} for s, c in
                          term_sessions[t].most_common(4)]}
            for t in top_terms
        ]
    else:
        jthought = []

    # Co-occupancy: term pairs co-mentioned within the same session (A.17).
    pair_counter = Counter()
    for sid, terms in sess_terms.items():
        ts = sorted(terms.keys())
        for i in range(len(ts)):
            for j in range(i + 1, len(ts)):
                a, b = ts[i], ts[j]
                if a in top_terms and b in top_terms:
                    pair_counter[(a, b)] += 1
    co_occ = [{"a": a, "b": b, "sessions": c} for (a, b), c in
              pair_counter.most_common(8) if c >= 2]

    terms = [{"term": t, "count": round(c, 2),
              "sessions": [{"id": s, "count": n} for s, n in
                           term_sessions[t].most_common(4)]}
             for t, c in reason_terms.most_common(14) if c >= 1.0]

    sys.stdout.write(json.dumps({
        "window": window,
        "now": now,
        "active_sessions": len(latest),
        "messages_scanned": messages_scanned,
        "reasoning_chars": reason_total_chars,
        "sessions": sessions,
        "memory_ops": sorted(memory_ops, key=lambda o: o["age"])[:15],
        "jthought": jthought,
        "concepts": {"projects": projects, "terms": terms,
                     "co_occ": co_occ},
    }))


if __name__ == "__main__":
    main()
