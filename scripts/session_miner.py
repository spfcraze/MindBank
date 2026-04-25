#!/usr/bin/env python3
"""
MindBank Session Miner

Mines Hermes session transcripts for knowledge and stores in MindBank.
Supports flat sessions folder and profile subfolders.

Usage:
    python3 session_miner.py [--profile PROFILE] [--dry-run] [--limit N]

Features:
    - Scans ~/.hermes/sessions/ for session_cron_*.json files
    - Extracts decisions, facts, problems, advice from assistant messages
    - Creates MindBank nodes via HTTP API with deduplication
    - Supports profile subfolders (e.g., ~/.hermes/sessions/rat/)
    - Tracks processed sessions in JSON state file
    - Skips PRAXIS boilerplate, skill dumps, and trivial content
"""

import argparse
import json
import os
import re
import sys
import urllib.request
import glob
import urllib.parse
from collections import Counter
from datetime import datetime
from pathlib import Path

# ── Configuration ──────────────────────────────────────────────────────────

DEFAULT_API = "http://127.0.0.1:8095/api/v1"
SESSIONS_DIR = Path.home() / ".hermes" / "sessions"
STATE_FILE = Path.home() / ".hermes" / "mindbank-miner-state.json"

# Node types we can create
VALID_TYPES = {"decision", "fact", "problem", "advice", "preference", "project", "session"}

# Signals for extraction
SIGNALS = {
    "decision": ["decided", "we'll use", "going with", "chose", "option", "done", "summary", "fixed", "applied"],
    "fact": ["is", "uses", "port", "version", "running on", "configured", "endpoint", "api"],
    "problem": ["bug", "broken", "fails", "error", "issue", "root cause", "workaround", "crash"],
    "advice": ["important", "note that", "gotcha", "lesson", "discovered", "should", "recommend"],
    "preference": ["prefer", "always use", "don't do", "user wants", "removed", "likes"],
    "project": ["repo", "project", "architecture", "stack", "deployment", "server"],
}

# Skip patterns
SKIP_PATTERNS = [
    r"\*\*CHECK \d+ — Inversion\*\*",
    r"\*\*GAP ANALYSIS\*\*",
    r"\[CONTEXT COMPACTION — REFERENCE ONLY\]",
    r"^\s*---\s*$",
    r"^name:\s+\S+",
    r"^description:",
    r"^version:\s*\d",
    r"MANDATORY",
    r"^\s*\*\*\s*$",
]

# ── HTTP API helpers ─────────────────────────────────────────────────────────

def api_call(path, body=None, method=None, api_base=DEFAULT_API):
    """Make HTTP API call to MindBank."""
    url = f"{api_base}{path}"
    data = json.dumps(body).encode() if body else None
    if method is None:
        method = "POST" if body else "GET"
    req = urllib.request.Request(url, data=data, method=method)
    req.add_header("Content-Type", "application/json")
    try:
        with urllib.request.urlopen(req, timeout=15) as r:
            return json.loads(r.read())
    except urllib.error.HTTPError as e:
        err_body = e.read().decode()
        return {"error": f"HTTP {e.code}: {err_body}"}
    except Exception as e:
        return {"error": str(e)}


def health_check(api_base=DEFAULT_API):
    """Check if MindBank API is reachable."""
    resp = api_call("/health", api_base=api_base)
    if "error" in resp:
        print(f"ERROR: MindBank API unreachable: {resp['error']}")
        return False
    print(f"MindBank API healthy: {resp}")
    return True


# ── State management ─────────────────────────────────────────────────────────

def load_state():
    """Load miner state from JSON file."""
    if STATE_FILE.exists():
        with open(STATE_FILE) as f:
            data = json.load(f)
        # Ensure all required keys exist (backward compat)
        defaults = {
            "processed_sessions": [],
            "processed_memory_files": [],
            "last_run": None,
            "nodes_created": 0,
            "edges_created": 0,
            "profiles": {},
        }
        for key, val in defaults.items():
            if key not in data:
                data[key] = val
        return data
    return {
        "processed_sessions": [],
        "processed_memory_files": [],
        "last_run": None,
        "nodes_created": 0,
        "edges_created": 0,
        "profiles": {},
    }


def save_state(state):
    """Save miner state to JSON file."""
    STATE_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(STATE_FILE, "w") as f:
        json.dump(state, f, indent=2)


# ── Session discovery ──────────────────────────────────────────────────────────

# Session file patterns to scan (in priority order)
SESSION_PATTERNS = ["session_cron_*.json", "session_*.json"]

def discover_sessions(sessions_dir, profile=None):
    """Find all session files matching known patterns, optionally in a profile subfolder."""
    if profile:
        # Profile subfolder is under ~/.hermes/profiles/<name>/sessions/
        search_dir = Path.home() / ".hermes" / "profiles" / profile / "sessions"
    else:
        search_dir = sessions_dir

    if not search_dir.exists():
        print(f"WARNING: Sessions directory not found: {search_dir}")
        return []

    files = []
    for pattern in SESSION_PATTERNS:
        matched = glob.glob(str(search_dir / pattern))
        files.extend(matched)

    # De-duplicate and sort by mtime (newest first)
    seen = set()
    unique = []
    for f in sorted(files, key=lambda x: os.path.getmtime(x), reverse=True):
        if f not in seen:
            seen.add(f)
            unique.append(f)
    return unique


def get_profile_sessions(sessions_dir):
    """Get sessions organized by profile. Returns dict: profile -> [files]."""
    result = {"default": []}

    # Flat files (no profile)
    flat = []
    for pattern in SESSION_PATTERNS:
        flat.extend(glob.glob(str(sessions_dir / pattern)))
    result["default"] = sorted(set(flat), key=lambda x: os.path.getmtime(x), reverse=True)

    # Profile subfolders under ~/.hermes/profiles/<name>/sessions/
    profiles_dir = Path.home() / ".hermes" / "profiles"
    if profiles_dir.exists():
        for subdir in profiles_dir.iterdir():
            if subdir.is_dir() and not subdir.name.startswith("."):
                profile_sessions_dir = subdir / "sessions"
                if profile_sessions_dir.exists():
                    profile_files = []
                    for pattern in SESSION_PATTERNS:
                        profile_files.extend(glob.glob(str(profile_sessions_dir / pattern)))
                    if profile_files:
                        result[subdir.name] = sorted(
                            set(profile_files), key=lambda x: os.path.getmtime(x), reverse=True
                        )

    return result


# ── Content extraction ────────────────────────────────────────────────────────

def should_skip_content(text):
    """Check if content should be skipped (boilerplate, etc)."""
    if not text or len(text.strip()) < 30:
        return True
    for pattern in SKIP_PATTERNS:
        if re.search(pattern, text, re.IGNORECASE):
            return True
    return False


def extract_knowledge_items(session_data, session_file):
    """Extract knowledge items from session messages."""
    items = []
    
    # Handle both dict-with-messages and list-of-messages formats
    if isinstance(session_data, dict):
        messages = session_data.get("messages", [])
    elif isinstance(session_data, list):
        messages = session_data
    else:
        messages = []
    
    if not messages:
        return items
    
    # Focus on assistant messages with substantial content
    assistant_msgs = [
        m for m in messages
        if m.get("role") == "assistant" and m.get("content")
    ]
    
    # Also look at tool results that contain memory operations
    tool_msgs = [
        m for m in messages
        if m.get("role") == "tool" and "memory" in m.get("content", "").lower()
    ]
    
    # Process assistant messages — focus on last 15 (summaries, decisions)
    for msg in assistant_msgs[-15:]:
        content = msg.get("content", "")
        if should_skip_content(content):
            continue
        
        # Split into paragraphs/sentences
        paragraphs = [p.strip() for p in content.split("\n\n") if p.strip()]
        
        for para in paragraphs:
            if len(para) < 40:
                continue
            
            # Check for signals
            para_lower = para.lower()
            for node_type, signals in SIGNALS.items():
                if any(s in para_lower for s in signals):
                    # Clean up the text
                    clean = para.replace("\n", " ").strip()
                    if len(clean) > 200:
                        clean = clean[:200] + "..."
                    items.append({
                        "type": node_type,
                        "text": clean,
                        "source": session_file,
                    })
                    break  # Only classify once per paragraph
    
    return items


def deduplicate_items(items):
    """Remove near-duplicate items."""
    seen = set()
    unique = []
    for item in items:
        # Normalize for dedup
        norm = re.sub(r"\s+", " ", item["text"].lower())[:80]
        if norm not in seen:
            seen.add(norm)
            unique.append(item)
    return unique


# ── Namespace detection ───────────────────────────────────────────────────────

def detect_namespace(text, profile=None, session_file=None):
    """Detect project namespace from content, session file path, or working directory references."""
    text_lower = text.lower()
    
    # Extract working directory from tool calls if present
    # Look for patterns like /home/rat/projectname or \\wsl.localhost\\Ubuntu\\home\\rat\\projectname
    wd_patterns = [
        r'/home/[^/\s]+/([a-zA-Z0-9_-]+)',
        r'\\\\wsl\.localhost\\[^\\]+\\home\\[^\\]+\\([a-zA-Z0-9_-]+)',
        r'~/([a-zA-Z0-9_-]+)',
    ]
    for pattern in wd_patterns:
        matches = re.findall(pattern, text)
        for match in matches:
            # Filter out common non-project directories
            if match not in ('.hermes', '.claude', '.config', '.local', '.ssh', '.git', '.github', '.vscode', 'node_modules', 'vendor', 'dist', 'build', 'tmp', 'temp', 'rat'):
                return match.lower()
    
    # Direct project mentions
    if "mindbank" in text_lower:
        return "mindbank"
    if "autowrkers" in text_lower or "ultraclaude" in text_lower:
        return "autowrkers"
    if "klixsor" in text_lower or "kataro" in text_lower:
        return "klixsor"
    if "grayswan" in text_lower or "grey swan" in text_lower:
        return "grayswan"
    if "polysports" in text_lower or "polymarket" in text_lower:
        return "polysports"
    if "hermes" in text_lower:
        return "hermes"

    # Session file path hint
    if session_file:
        session_lower = session_file.lower()
        for project in ['mindbank', 'autowrkers', 'klixsor', 'kataro', 'grayswan', 'polysports', 'polymarket']:
            if project in session_lower:
                return project

    # Profile fallback - but only if it's a real project name, not a username
    if profile and profile not in ("default", "rat", "user", "home"):
        return profile

    return "global"


# ── MindBank operations ──────────────────────────────────────────────────────

def search_mindbank(query, namespace=None, api_base=DEFAULT_API):
    """Search MindBank for existing nodes."""
    path = f"/search?q={urllib.parse.quote(query)}&limit=5"
    if namespace:
        path += f"&namespace={urllib.parse.quote(namespace)}"
    resp = api_call(path, api_base=api_base)
    if isinstance(resp, list):
        return resp
    if isinstance(resp, dict) and "error" in resp:
        return []
    return resp if isinstance(resp, list) else []


def create_node(label, node_type, content, namespace="global", profile=None, api_base=DEFAULT_API):
    """Create a node in MindBank."""
    if node_type not in VALID_TYPES:
        node_type = "fact"

    body = {
        "label": label[:100],
        "node_type": node_type,
        "content": content[:2000],
        "namespace": namespace,
    }
    
    # Add profile to metadata if provided
    if profile and profile != "default":
        body["metadata"] = {"profile": profile}
    
    resp = api_call("/nodes", body=body, api_base=api_base)
    if "error" in resp:
        print(f"  ERROR creating node: {resp['error']}")
        return None
    return resp.get("id")


def create_edge(source_id, target_id, edge_type="relates_to", api_base=DEFAULT_API):
    """Create an edge between nodes."""
    body = {
        "source_id": source_id,
        "target_id": target_id,
        "edge_type": edge_type,
    }
    resp = api_call("/edges", body=body, api_base=api_base)
    if "error" in resp:
        if "duplicate" in str(resp.get("error", "")).lower():
            return True  # Already exists
        print(f"  ERROR creating edge: {resp['error']}")
        return False
    return True


# ── Memory file mining ──────────────────────────────────────────────────────

MEMORY_FILES = {
    "default": {
        "memory": Path.home() / ".hermes" / "memories" / "MEMORY.md",
        "user": Path.home() / ".hermes" / "memories" / "USER.md",
    }
}


def discover_memory_files():
    """Discover all MEMORY.md and USER.md files, including profile-specific ones."""
    result = {}

    # Default (flat) memory files
    flat = {}
    for key, path in MEMORY_FILES["default"].items():
        if path.exists():
            flat[key] = path
    if flat:
        result["default"] = flat

    # Profile subfolders
    profiles_dir = Path.home() / ".hermes" / "profiles"
    if profiles_dir.exists():
        for subdir in profiles_dir.iterdir():
            if subdir.is_dir() and not subdir.name.startswith("."):
                mem_dir = subdir / "memories"
                if mem_dir.exists():
                    profile_files = {}
                    for key in ("memory", "user"):
                        path = mem_dir / ("MEMORY.md" if key == "memory" else "USER.md")
                        if path.exists():
                            profile_files[key] = path
                    if profile_files:
                        result[subdir.name] = profile_files

    return result


def parse_memory_file(filepath):
    """Parse a MEMORY.md or USER.md file into entries."""
    entries = []
    try:
        with open(filepath, "r", encoding="utf-8") as f:
            content = f.read()
    except Exception as e:
        print(f"  ERROR reading {filepath}: {e}")
        return entries

    # Split on § separator
    raw_entries = content.split("§")

    for entry in raw_entries:
        entry = entry.strip()
        if not entry or len(entry) < 30:
            continue

        # Skip if it's just metadata lines
        lines = [l.strip() for l in entry.split("\n") if l.strip()]
        if not lines:
            continue

        # Detect node type from content
        node_type = "fact"
        entry_lower = entry.lower()

        # Check for explicit markers like [fact], [decision], etc.
        marker_match = re.search(r'\[(fact|decision|problem|advice|preference|project)\]', entry, re.IGNORECASE)
        if marker_match:
            node_type = marker_match.group(1).lower()
        else:
            # Infer from content signals
            for nt, signals in SIGNALS.items():
                if any(s in entry_lower for s in signals):
                    node_type = nt
                    break

        # Generate label from first sentence or first 80 chars
        first_line = lines[0]
        # Remove markdown formatting
        clean_line = re.sub(r'[#*_`]', '', first_line).strip()
        label = clean_line[:80]
        if len(label) < 10:
            label = entry[:80].replace("\n", " ").strip()

        entries.append({
            "type": node_type,
            "label": label,
            "text": entry,
            "source": str(filepath),
        })

    return entries


def mine_memory_files(dry_run=False, api_base=DEFAULT_API):
    """Mine MEMORY.md and USER.md for static knowledge."""
    state = load_state()
    processed = set(state.get("processed_memory_files", []))

    memory_files = discover_memory_files()
    if not memory_files:
        print("No memory files found.")
        return 0, 0

    total_nodes = 0
    total_edges = 0

    for profile, files in memory_files.items():
        print(f"\n{'='*60}")
        print(f"Memory Profile: {profile}")
        print(f"{'='*60}")

        for file_type, filepath in files.items():
            file_id = f"{profile}:{file_type}:{filepath}"
            if file_id in processed:
                print(f"  SKIP (already processed): {filepath.name}")
                continue

            print(f"\n  Processing: {filepath.name}")
            entries = parse_memory_file(filepath)

            if not entries:
                print(f"    No extractable entries found")
                state["processed_memory_files"].append(file_id)
                continue

            print(f"    Extracted {len(entries)} entries")

            if dry_run:
                for entry in entries[:3]:
                    print(f"      [{entry['type']}] {entry['label'][:60]}...")
                state["processed_memory_files"].append(file_id)
                continue

            # Create nodes
            created_ids = []
            for entry in entries:
                namespace = detect_namespace(entry["text"], profile, str(filepath))
                label = entry["label"]

                # Deduplication check
                existing = search_mindbank(label[:40], namespace=namespace, api_base=api_base)
                if existing:
                    print(f"    SKIP (duplicate): {label[:60]}...")
                    continue

                node_id = create_node(
                    label=label,
                    node_type=entry["type"],
                    content=entry["text"],
                    namespace=namespace,
                    profile=profile,
                    api_base=api_base,
                )
                if node_id:
                    created_ids.append(node_id)
                    total_nodes += 1
                    print(f"    CREATED [{entry['type']}] {label[:60]}...")

            # Create edges between nodes from same file
            for i in range(len(created_ids) - 1):
                create_edge(
                    created_ids[i], created_ids[i + 1],
                    edge_type="relates_to", api_base=api_base
                )
                total_edges += 1

            state["processed_memory_files"].append(file_id)

    # Update state
    state["last_run"] = datetime.now().isoformat()
    state["nodes_created"] = state.get("nodes_created", 0) + total_nodes
    state["edges_created"] = state.get("edges_created", 0) + total_edges
    save_state(state)

    print(f"\n{'='*60}")
    print(f"MEMORY MINING COMPLETE")
    print(f"{'='*60}")
    print(f"Nodes created: {total_nodes}")
    print(f"Edges created: {total_edges}")

    return total_nodes, total_edges


# ── Main mining logic ────────────────────────────────────────────────────────

def mine_sessions(profile=None, dry_run=False, limit=None, api_base=DEFAULT_API):
    """Mine sessions for knowledge and store in MindBank."""
    state = load_state()

    # Health check
    if not dry_run and not health_check(api_base):
        return False

    # Discover sessions
    if profile:
        files = discover_sessions(SESSIONS_DIR, profile=profile)
        profiles = {profile: files} if files else {}
    else:
        profiles = get_profile_sessions(SESSIONS_DIR)

    total_processed = 0
    total_nodes = 0
    total_edges = 0

    for prof_name, files in profiles.items():
        if not files:
            continue

        print(f"\n{'='*60}")
        print(f"Profile: {prof_name} ({len(files)} sessions)")
        print(f"{'='*60}")

        # Filter out already processed
        processed = set(state.get("processed_sessions", []))
        unprocessed = [f for f in files if os.path.basename(f) not in processed]

        if limit:
            unprocessed = unprocessed[:limit]

        print(f"Unprocessed: {len(unprocessed)} (already processed: {len(processed)})")

        for session_file in unprocessed:
            basename = os.path.basename(session_file)
            print(f"\n  Processing: {basename}")

            try:
                with open(session_file) as f:
                    session_data = json.load(f)
            except Exception as e:
                print(f"    ERROR reading file: {e}")
                continue

            # Extract knowledge
            items = extract_knowledge_items(session_data, basename)
            items = deduplicate_items(items)

            if not items:
                print(f"    No extractable knowledge found")
                state["processed_sessions"].append(basename)
                continue

            print(f"    Extracted {len(items)} knowledge items")

            if dry_run:
                for item in items[:3]:
                    print(f"      [{item['type']}] {item['text'][:80]}...")
                state["processed_sessions"].append(basename)
                continue

            # Create nodes
            created_ids = []
            for item in items[:10]:  # Cap at 10 per session
                namespace = detect_namespace(item["text"], prof_name, session_file)
                label = item["text"][:80]

                # Deduplication check
                existing = search_mindbank(label[:40], namespace=namespace, api_base=api_base)
                if existing:
                    print(f"    SKIP (duplicate): {label[:60]}...")
                    continue

                node_id = create_node(
                    label=label,
                    node_type=item["type"],
                    content=item["text"],
                    namespace=namespace,
                    profile=prof_name,
                    api_base=api_base,
                )
                if node_id:
                    created_ids.append(node_id)
                    total_nodes += 1
                    print(f"    CREATED [{item['type']}] {label[:60]}...")

            # Create edges between nodes from same session
            for i in range(len(created_ids) - 1):
                create_edge(
                    created_ids[i], created_ids[i + 1],
                    edge_type="relates_to", api_base=api_base
                )
                total_edges += 1

            state["processed_sessions"].append(basename)
            total_processed += 1

    # Update state
    state["last_run"] = datetime.now().isoformat()
    state["nodes_created"] = state.get("nodes_created", 0) + total_nodes
    state["edges_created"] = state.get("edges_created", 0) + total_edges
    save_state(state)

    print(f"\n{'='*60}")
    print(f"MINING COMPLETE")
    print(f"{'='*60}")
    print(f"Sessions processed: {total_processed}")
    print(f"Nodes created: {total_nodes}")
    print(f"Edges created: {total_edges}")
    print(f"State saved to: {STATE_FILE}")

    return True


# ── CLI ──────────────────────────────────────────────────────────────────────

def main():
    parser = argparse.ArgumentParser(description="MindBank Session Miner")
    parser.add_argument("--profile", help="Process sessions for specific profile")
    parser.add_argument("--dry-run", action="store_true", help="Show what would be created")
    parser.add_argument("--limit", type=int, help="Max sessions to process")
    parser.add_argument("--api", default=DEFAULT_API, help="MindBank API base URL")
    parser.add_argument("--reset-state", action="store_true", help="Reset processed tracking")
    parser.add_argument("--list-profiles", action="store_true", help="List available profiles")
    parser.add_argument("--mine-memories", action="store_true", help="Mine MEMORY.md and USER.md files")
    parser.add_argument("--mine-all", action="store_true", help="Mine both sessions and memory files")
    args = parser.parse_args()

    if args.reset_state:
        if STATE_FILE.exists():
            STATE_FILE.unlink()
        print("State reset. All sessions and memory files will be re-processed.")
        return

    if args.list_profiles:
        profiles = get_profile_sessions(SESSIONS_DIR)
        print("Available profiles:")
        for name, files in profiles.items():
            print(f"  {name}: {len(files)} sessions")
        return

    # Import urllib.parse for search
    import urllib.parse
    globals()["urllib"] = __import__("urllib")

    # Mine memory files if requested
    if args.mine_memories or args.mine_all:
        if not args.dry_run and not health_check(args.api):
            sys.exit(1)
        mem_nodes, mem_edges = mine_memory_files(dry_run=args.dry_run, api_base=args.api)
        if not args.mine_all:
            sys.exit(0 if mem_nodes > 0 or mem_edges > 0 else 0)

    # Mine sessions
    success = mine_sessions(
        profile=args.profile,
        dry_run=args.dry_run,
        limit=args.limit,
        api_base=args.api,
    )
    sys.exit(0 if success else 1)


if __name__ == "__main__":
    main()
