#!/usr/bin/env python3
"""Mine Hermes .jsonl session files into MindBank.

Hermes stores actual session transcripts as .jsonl files where each line
is a JSON object with role, content, timestamp, etc.
"""
import json
import os
import re
import urllib.request
import urllib.error
from pathlib import Path
from datetime import datetime

DEFAULT_API = "http://127.0.0.1:8095/api/v1"
SESSION_DIR = Path.home() / ".hermes" / "sessions"
WATCHED_FILE = Path.home() / ".hermes" / ".jsonl_session_watcher_state.json"


def load_watched_sessions() -> set:
    if WATCHED_FILE.exists():
        try:
            with open(WATCHED_FILE, 'r') as f:
                return set(json.load(f))
        except:
            pass
    return set()


def save_watched_sessions(watched: set):
    WATCHED_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(WATCHED_FILE, 'w') as f:
        json.dump(list(watched), f)


def find_jsonl_sessions() -> list[Path]:
    if not SESSION_DIR.exists():
        return []
    return sorted(SESSION_DIR.glob("*.jsonl"), key=lambda p: p.stat().st_mtime, reverse=True)


def create_node(api_base: str, node_type: str, label: str, content: str,
                workspace: str = "hermes", namespace: str = "global",
                summary: str = "", metadata: dict | None = None) -> str | None:
    payload: dict = {
        "node_type": node_type,
        "label": label,
        "content": content[:10000] if content else "",
        "workspace_name": workspace,
        "namespace": namespace,
    }
    if summary:
        payload["summary"] = summary
    if metadata is not None:
        payload["metadata"] = metadata

    data = json.dumps(payload).encode()
    req = urllib.request.Request(
        f"{api_base}/nodes",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            result = json.loads(resp.read())
            return result.get("id")
    except urllib.error.HTTPError as e:
        print(f"  HTTP Error creating node: {e.code} {e.read().decode()[:200]}")
        return None
    except Exception as e:
        print(f"  Error creating node: {e}")
        return None


def create_edge(api_base: str, source: str, target: str, edge_type: str,
                workspace: str = "hermes") -> bool:
    data = json.dumps({
        "source_id": source,
        "target_id": target,
        "edge_type": edge_type,
        "workspace_name": workspace,
    }).encode()
    req = urllib.request.Request(
        f"{api_base}/edges",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return True
    except Exception as e:
        print(f"  Error creating edge: {e}")
        return False


def extract_knowledge(content: str) -> list[dict]:
    knowledge = []
    lines = content.split("\n")
    for line in lines:
        line = line.strip()
        if len(line) < 20 or len(line) > 500:
            continue
        # Decisions
        if re.search(r'(?i)(decided|decision|we will|let\'s|conclusion)', line):
            knowledge.append({"type": "decision", "label": line[:100], "content": line})
            continue
        # Facts
        if re.search(r'(?i)(fact|note|remember|important)', line):
            knowledge.append({"type": "fact", "label": line[:100], "content": line})
            continue
        # Problems
        if re.search(r'(?i)(problem|issue|bug|error|fail|broken)', line):
            knowledge.append({"type": "problem", "label": line[:100], "content": line})
            continue
        # Preferences
        if re.search(r'(?i)(prefer|should use|recommend)', line):
            knowledge.append({"type": "preference", "label": line[:100], "content": line})
            continue
    # Deduplicate
    seen = set()
    unique = []
    for item in knowledge:
        key = item["content"].lower()[:50]
        if key not in seen:
            seen.add(key)
            unique.append(item)
    return unique[:20]


def mine_jsonl_session(session_path: Path, api_base: str = DEFAULT_API) -> bool:
    try:
        with open(session_path, 'r', encoding='utf-8') as f:
            lines = f.readlines()
    except Exception as e:
        print(f"  ✗ Failed to read {session_path.name}: {e}")
        return False

    # Parse JSONL
    messages = []
    for line in lines:
        line = line.strip()
        if not line:
            continue
        try:
            msg = json.loads(line)
            messages.append(msg)
        except json.JSONDecodeError:
            continue

    # Extract assistant content
    content_parts = []
    for msg in messages:
        if msg.get("role") == "assistant" and msg.get("content"):
            content = msg["content"]
            if isinstance(content, str) and len(content.strip()) > 10:
                content_parts.append(content.strip())

    content = "\n\n".join(content_parts)
    if not content:
        print(f"  ⊘ No assistant content: {session_path.name}")
        return True  # Not a failure, just nothing to mine

    # Derive namespace from first user message with cwd or from filename
    namespace = "global"
    for msg in messages:
        if msg.get("role") == "user":
            # Check for cwd in the session context (not in user message usually)
            pass

    # Use filename stem as label
    label = session_path.stem
    # Try to find a better label from session metadata
    for msg in messages:
        if msg.get("role") == "session_meta":
            # Could extract info here
            pass

    # Truncate content for node
    content_for_node = content[:10000] if len(content) > 10000 else content
    summary = f"Session mined on {datetime.now().isoformat()}"
    metadata = {"source_file": str(session_path), "message_count": len(messages)}

    print(f"  Mining: {label} ({len(messages)} messages, {len(content)} chars)")

    # Create session node
    session_id = create_node(api_base, "session", label, content_for_node,
                              workspace="hermes", namespace=namespace,
                              summary=summary, metadata=metadata)
    if not session_id:
        print(f"  ✗ Failed to create session node for {session_path.name}")
        return False

    # Extract and create knowledge nodes
    knowledge = extract_knowledge(content)
    mined_knowledge = 0
    for item in knowledge:
        node_id = create_node(api_base, item["type"], item["label"], item["content"],
                              workspace="hermes", namespace=namespace,
                              summary=item["content"][:200])
        if node_id:
            create_edge(api_base, session_id, node_id, "produced", workspace="hermes")
            mined_knowledge += 1

    print(f"  ✓ Mined: {label} + {mined_knowledge} knowledge items")
    return True


def main():
    watched = load_watched_sessions()
    sessions = find_jsonl_sessions()

    new_sessions = [s for s in sessions if str(s) not in watched]
    if not new_sessions:
        print(f"[{datetime.now().isoformat()}] No new .jsonl sessions to mine")
        print(f"  Total .jsonl sessions tracked: {len(watched)}")
        return 0

    print(f"[{datetime.now().isoformat()}] Found {len(new_sessions)} new .jsonl sessions to mine")

    mined = 0
    failed = 0
    for session_path in new_sessions:
        if mine_jsonl_session(session_path):
            watched.add(str(session_path))
            mined += 1
        else:
            failed += 1

    save_watched_sessions(watched)
    print(f"[{datetime.now().isoformat()}] Results: {mined} mined, {failed} failed, {len(new_sessions)} total")
    return 0 if failed == 0 else 1


if __name__ == "__main__":
    exit(main())
