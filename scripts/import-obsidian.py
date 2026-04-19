#!/usr/bin/env python3
"""Import Obsidian vault into MindBank.

Walks an Obsidian vault, parses markdown notes (YAML frontmatter, wikilinks,
tags), and creates MindBank nodes and edges via the REST API.

Usage:
    python3 import-obsidian.py [VAULT_PATH] [OPTIONS]

Options:
    --namespace NAME    MindBank namespace (default: folder name or "obsidian")
    --api-url URL       MindBank API URL (default: http://localhost:8095/api/v1)
    --dry-run           Show what would be imported without creating anything
    --resume            Skip files already imported (uses state file)
    --state-file PATH   Path to state file (default: .obsidian-import-state.json)
    --error-log PATH    Path to error log (default: import-obsidian-errors.json)
    --verbose           Enable debug logging
"""

import argparse
import hashlib
import json
import logging
import os
import re
import sys
import time
from pathlib import Path
from typing import Any, Dict, List, Optional, Set, Tuple
from urllib import request, error

# ---------------------------------------------------------------------------
# Defaults
# ---------------------------------------------------------------------------

DEFAULT_API_URL = "http://localhost:8095/api/v1"
DEFAULT_VAULT = os.environ.get("OBSIDIAN_VAULT_PATH", os.path.expanduser("~/Documents/Obsidian Vault"))
BATCH_SIZE = 100
MAX_RETRIES = 3
RETRY_DELAY = 2  # seconds

# Folders to skip
SKIP_DIRS = {".obsidian", ".trash", ".git", "node_modules", "__pycache__"}

# Node type keywords (frontmatter tags or inline tags → node type)
TAG_TYPE_MAP = {
    "project": "project",
    "decision": "decision",
    "fact": "fact",
    "preference": "preference",
    "problem": "problem",
    "advice": "advice",
    "topic": "topic",
    "person": "person",
    "agent": "agent",
    "event": "event",
    "concept": "concept",
    "question": "question",
}

# ---------------------------------------------------------------------------
# Logging
# ---------------------------------------------------------------------------

logging.basicConfig(
    level=logging.INFO,
    format="%(asctime)s [%(levelname)s] %(message)s",
    datefmt="%H:%M:%S",
)
log = logging.getLogger("import-obsidian")

# ---------------------------------------------------------------------------
# API helpers
# ---------------------------------------------------------------------------


def api_call(base_url: str, method: str, path: str, body: dict = None,
             timeout: int = 30) -> dict:
    """Make an HTTP call to MindBank API with retry."""
    url = base_url.rstrip("/") + path
    data = json.dumps(body).encode() if body else None

    for attempt in range(1, MAX_RETRIES + 1):
        try:
            req = request.Request(url, data=data, method=method)
            req.add_header("Content-Type", "application/json")
            with request.urlopen(req, timeout=timeout) as resp:
                return json.loads(resp.read())
        except error.HTTPError as e:
            body_text = e.read().decode(errors="replace")
            if e.code == 409:  # Conflict — duplicate
                return {"error": "duplicate", "status": 409}
            if attempt < MAX_RETRIES:
                log.warning("API %s %s returned %d (attempt %d/%d): %s",
                            method, path, e.code, attempt, MAX_RETRIES, body_text[:200])
                time.sleep(RETRY_DELAY * attempt)
                continue
            return {"error": f"HTTP {e.code}: {body_text[:200]}"}
        except Exception as e:
            if attempt < MAX_RETRIES:
                log.warning("API %s %s failed (attempt %d/%d): %s",
                            method, path, attempt, MAX_RETRIES, e)
                time.sleep(RETRY_DELAY * attempt)
                continue
            return {"error": str(e)}
    return {"error": "max retries exceeded"}


def check_api_health(base_url: str) -> bool:
    """Verify MindBank API is reachable."""
    try:
        result = api_call(base_url, "GET", "/health", timeout=5)
        if result.get("status") == "ok":
            log.info("MindBank API healthy (v%s)", result.get("version", "?"))
            return True
        log.error("MindBank API unhealthy: %s", result)
        return False
    except Exception as e:
        log.error("MindBank API unreachable: %s", e)
        return False


# ---------------------------------------------------------------------------
# Obsidian parsing
# ---------------------------------------------------------------------------


def parse_frontmatter(content: str) -> Tuple[dict, str]:
    """Extract YAML frontmatter and return (metadata_dict, body).

    Returns empty dict if no frontmatter found.
    """
    if not content.startswith("---"):
        return {}, content

    # Find closing ---
    end = content.find("\n---", 3)
    if end == -1:
        return {}, content

    yaml_block = content[4:end].strip()
    body = content[end + 4:].strip()

    # Simple YAML parser (handles key: value, lists, nested basics)
    metadata = {}
    current_key = None
    current_list = None

    for line in yaml_block.split("\n"):
        line = line.rstrip()
        if not line or line.startswith("#"):
            continue

        # List item
        if line.startswith("  - "):
            if current_key and current_list is not None:
                val = line[4:].strip().strip('"').strip("'")
                current_list.append(val)
            continue

        # Key: value
        match = re.match(r"^(\w[\w-]*):\s*(.*)", line)
        if match:
            # Save previous list
            if current_key and current_list is not None:
                metadata[current_key] = current_list
                current_list = None

            key = match.group(1).lower()
            val = match.group(2).strip().strip('"').strip("'")

            if val == "" or val is None:
                # Start of list
                current_key = key
                current_list = []
            else:
                # Scalar value
                # Type coercion
                if val.lower() in ("true", "yes"):
                    metadata[key] = True
                elif val.lower() in ("false", "no"):
                    metadata[key] = False
                elif re.match(r"^-?\d+$", val):
                    metadata[key] = int(val)
                elif re.match(r"^-?\d+\.\d+$", val):
                    metadata[key] = float(val)
                else:
                    metadata[key] = val
                current_key = key

    # Save last list
    if current_key and current_list is not None:
        metadata[current_key] = current_list

    return metadata, body


def extract_wikilinks(content: str) -> List[str]:
    """Extract [[wikilinks]] from markdown content.

    Handles:
    - [[Note Name]]
    - [[Note Name|Alias]]
    - [[Note Name#heading]]
    - [[Note Name#heading|Alias]]
    """
    links = []
    for match in re.finditer(r"\[\[([^\]|#]+)(?:#[^\]|]*)?(?:\|[^\]]*)?\]\]", content):
        link = match.group(1).strip()
        if link:
            links.append(link)
    return links


def extract_tags(text: str) -> Set[str]:
    """Extract #tags from text (inline, not YAML)."""
    tags = set()
    for match in re.finditer(r"(?:^|\s)#([a-zA-Z][\w-]*)", text):
        tags.add(match.group(1).lower())
    return tags


def detect_node_type(metadata: dict, tags: Set[str], folder: str) -> str:
    """Determine MindBank node type from metadata, tags, and folder.

    Priority:
    1. frontmatter 'type' field
    2. tags matching known types
    3. folder name matching known types
    4. default: 'fact'
    """
    # Frontmatter type
    fm_type = metadata.get("type", "").lower().strip()
    if fm_type in TAG_TYPE_MAP:
        return TAG_TYPE_MAP[fm_type]

    # Tags
    for tag in tags:
        tag_lower = tag.lower()
        if tag_lower in TAG_TYPE_MAP:
            return TAG_TYPE_MAP[tag_lower]

    # Folder name
    folder_lower = folder.lower().strip("/")
    if folder_lower in TAG_TYPE_MAP:
        return TAG_TYPE_MAP[folder_lower]

    return "fact"


def file_hash(filepath: str) -> str:
    """SHA256 hash of file contents."""
    with open(filepath, "rb") as f:
        return hashlib.sha256(f.read()).hexdigest()


# ---------------------------------------------------------------------------
# State management (for resume)
# ---------------------------------------------------------------------------


def load_state(state_file: str) -> dict:
    """Load import state from disk."""
    if os.path.exists(state_file):
        try:
            with open(state_file) as f:
                return json.load(f)
        except Exception:
            pass
    return {"version": 1, "imported": {}, "last_run": None}


def save_state(state_file: str, state: dict) -> None:
    """Save import state to disk."""
    state["last_run"] = time.strftime("%Y-%m-%dT%H:%M:%S")
    with open(state_file, "w") as f:
        json.dump(state, f, indent=2)


# ---------------------------------------------------------------------------
# Error logging
# ---------------------------------------------------------------------------


class ErrorLogger:
    """Collects errors and writes them to a JSON file."""

    def __init__(self, path: str):
        self.path = path
        self.errors: List[dict] = []

    def add(self, stage: str, file: str, error_msg: str, details: dict = None):
        entry = {
            "timestamp": time.strftime("%Y-%m-%dT%H:%M:%S"),
            "stage": stage,
            "file": file,
            "error": error_msg,
        }
        if details:
            entry["details"] = details
        self.errors.append(entry)
        log.error("[%s] %s: %s", stage, file, error_msg)

    def save(self):
        if self.errors:
            with open(self.path, "w") as f:
                json.dump(self.errors, f, indent=2)
            log.warning("%d errors saved to %s", len(self.errors), self.path)

    @property
    def count(self):
        return len(self.errors)


# ---------------------------------------------------------------------------
# Main import logic
# ---------------------------------------------------------------------------


def discover_notes(vault_path: str) -> List[str]:
    """Find all .md files in vault, excluding skip dirs."""
    notes = []
    vault = Path(vault_path)
    for md in vault.rglob("*.md"):
        # Check if any parent is in skip dirs
        parts = md.relative_to(vault).parts
        if any(p in SKIP_DIRS or p.startswith(".") for p in parts):
            continue
        notes.append(str(md))
    notes.sort()
    return notes


def parse_note(filepath: str, vault_path: str) -> Optional[dict]:
    """Parse a single Obsidian note into a structured dict.

    Returns None if file can't be read.
    """
    try:
        with open(filepath, encoding="utf-8", errors="replace") as f:
            content = f.read()
    except Exception as e:
        return None

    metadata, body = parse_frontmatter(content)
    wikilinks = extract_wikilinks(content)
    inline_tags = extract_tags(body)
    fm_tags = metadata.get("tags", [])
    if isinstance(fm_tags, str):
        fm_tags = [fm_tags]
    all_tags = inline_tags | set(t.lower() for t in fm_tags)

    # Relative path for namespace/folder detection
    rel_path = os.path.relpath(filepath, vault_path)
    folder = os.path.dirname(rel_path)
    label = Path(filepath).stem

    node_type = detect_node_type(metadata, all_tags, folder)

    # Summary = first non-empty line of body, truncated
    summary = ""
    for line in body.split("\n"):
        line = line.strip()
        if line and not line.startswith("#"):
            summary = line[:200]
            break

    return {
        "filepath": filepath,
        "label": label,
        "node_type": node_type,
        "content": body[:2000],  # Cap content length
        "summary": summary,
        "metadata": metadata,
        "tags": list(all_tags),
        "wikilinks": wikilinks,
        "folder": folder,
        "hash": file_hash(filepath),
    }


def resolve_namespace(args_namespace: str, folder: str) -> str:
    """Determine namespace for a note."""
    if args_namespace:
        return args_namespace
    # Use top-level folder name
    parts = folder.strip("/").split("/")
    if parts and parts[0]:
        return parts[0].lower().replace(" ", "-")
    return "obsidian"


def build_topic_nodes(notes: List[dict], namespace: str) -> List[dict]:
    """Create topic nodes for unique folders."""
    folders = set()
    for n in notes:
        if n["folder"]:
            # Each path segment is a topic
            parts = n["folder"].strip("/").split("/")
            for i in range(len(parts)):
                folders.add("/".join(parts[:i + 1]))

    topics = []
    for folder in sorted(folders):
        name = folder.split("/")[-1]
        topics.append({
            "label": name,
            "node_type": "topic",
            "content": f"Folder: {folder}",
            "summary": f"Topic folder: {name}",
            "namespace": namespace,
            "folder": folder,
            "is_topic": True,
        })
    return topics


def build_tag_edges(notes: List[dict], node_ids: Dict[str, str]) -> List[dict]:
    """Create edges between notes that share tags."""
    # tag → list of note labels
    tag_notes: Dict[str, List[str]] = {}
    for n in notes:
        for tag in n.get("tags", []):
            tag_notes.setdefault(tag, []).append(n["label"])

    edges = []
    seen = set()
    for tag, labels in tag_notes.items():
        # Connect all pairs (limit to avoid explosion)
        for i, la in enumerate(labels[:10]):  # max 10 per tag
            for lb in labels[i + 1:11]:
                pair = tuple(sorted([la, lb]))
                if pair not in seen and la in node_ids and lb in node_ids:
                    seen.add(pair)
                    edges.append({
                        "source_id": node_ids[la],
                        "target_id": node_ids[lb],
                        "edge_type": "relates_to",
                        "weight": 0.5,
                    })
    return edges


def find_existing_node(base_url: str, label: str, namespace: str) -> Optional[str]:
    """Check if a node with this label already exists. Returns node_id or None."""
    result = api_call(base_url, "POST", "/search/hybrid", {
        "query": label,
        "namespace": namespace,
        "limit": 3,
    }, timeout=10)

    if isinstance(result, list):
        for r in result:
            if r.get("label", "").lower().strip() == label.lower().strip():
                return r.get("node_id") or r.get("id")
    return None


def batch_create_nodes(base_url: str, nodes: List[dict],
                       error_log: ErrorLogger, dry_run: bool = False) -> Dict[str, str]:
    """Create nodes in batches. Returns {label: node_id}."""
    label_to_id = {}

    for i in range(0, len(nodes), BATCH_SIZE):
        batch = nodes[i:i + BATCH_SIZE]

        if dry_run:
            for n in batch:
                log.info("[DRY-RUN] Would create node: [%s] %s", n["node_type"], n["label"])
                label_to_id[n["label"]] = f"dry-{n['label']}"
            continue

        # Build request body
        node_creates = []
        for n in batch:
            nc = {
                "label": n["label"],
                "node_type": n["node_type"],
                "content": n.get("content", ""),
                "summary": n.get("summary", ""),
                "namespace": n.get("namespace", "obsidian"),
            }
            # Add metadata as JSON if present
            if n.get("metadata"):
                nc["metadata"] = json.dumps(n["metadata"])
            node_creates.append(nc)

        result = api_call(base_url, "POST", "/nodes/batch", {"nodes": node_creates}, timeout=60)

        if "error" in result and "errors" not in result:
            error_log.add("upload", "batch", result["error"])
            continue

        # Process results
        created = result.get("created", result.get("nodes", []))
        errors_list = result.get("errors", [])

        if isinstance(created, list):
            for node in created:
                label = node.get("label", "")
                node_id = node.get("id", "")
                if label and node_id:
                    label_to_id[label] = node_id

        for err in errors_list:
            error_log.add("upload", "batch", str(err))

        log.info("Batch %d-%d: %d created, %d errors",
                 i, i + len(batch), len(created) if isinstance(created, list) else 0,
                 len(errors_list))

    return label_to_id


def batch_create_edges(base_url: str, edges: List[dict],
                       error_log: ErrorLogger, dry_run: bool = False) -> int:
    """Create edges in batches. Returns count of created edges."""
    created_count = 0

    for i in range(0, len(edges), BATCH_SIZE):
        batch = edges[i:i + BATCH_SIZE]

        if dry_run:
            for e in batch:
                log.info("[DRY-RUN] Would create edge: %s → %s (%s)",
                         e["source_id"][:8], e["target_id"][:8], e["edge_type"])
            created_count += len(batch)
            continue

        result = api_call(base_url, "POST", "/edges/batch", {"edges": batch}, timeout=60)

        if "error" in result and "errors" not in result:
            error_log.add("upload", "edges-batch", result["error"])
            continue

        created = result.get("created", 0)
        if isinstance(created, list):
            created = len(created)
        created_count += created

        errors_list = result.get("errors", [])
        for err in errors_list:
            error_log.add("upload", "edge", str(err))

    return created_count


# ---------------------------------------------------------------------------
# Main
# ---------------------------------------------------------------------------


def main():
    parser = argparse.ArgumentParser(description="Import Obsidian vault into MindBank")
    parser.add_argument("vault", nargs="?", default=DEFAULT_VAULT,
                        help=f"Path to Obsidian vault (default: {DEFAULT_VAULT})")
    parser.add_argument("--namespace", "-n", default="",
                        help="MindBank namespace (default: top-level folder or 'obsidian')")
    parser.add_argument("--api-url", default=DEFAULT_API_URL,
                        help=f"MindBank API URL (default: {DEFAULT_API_URL})")
    parser.add_argument("--dry-run", action="store_true",
                        help="Show what would be imported without creating anything")
    parser.add_argument("--resume", action="store_true",
                        help="Skip files already imported (uses state file)")
    parser.add_argument("--state-file", default=".obsidian-import-state.json",
                        help="Path to state file")
    parser.add_argument("--error-log", default="import-obsidian-errors.json",
                        help="Path to error log")
    parser.add_argument("--verbose", "-v", action="store_true",
                        help="Enable debug logging")
    args = parser.parse_args()

    if args.verbose:
        logging.getLogger().setLevel(logging.DEBUG)

    vault_path = os.path.expanduser(args.vault)
    if not os.path.isdir(vault_path):
        log.error("Vault directory not found: %s", vault_path)
        log.error("Set OBSIDIAN_VAULT_PATH env var or pass path as argument")
        sys.exit(1)

    log.info("=== Obsidian → MindBank Import ===")
    log.info("Vault: %s", vault_path)
    log.info("API: %s", args.api_url)
    if args.dry_run:
        log.info("MODE: DRY RUN (no changes)")

    # Check API health
    if not args.dry_run:
        if not check_api_health(args.api_url):
            log.error("Cannot reach MindBank API. Is it running?")
            sys.exit(1)

    err_log = ErrorLogger(args.error_log)

    # Load state for resume
    state = load_state(args.state_file) if args.resume else {"imported": {}}

    # Stage 1: Discover
    log.info("Stage 1: Discovering notes...")
    all_notes = discover_notes(vault_path)
    log.info("Found %d .md files", len(all_notes))

    if not all_notes:
        log.warning("No .md files found in vault")
        sys.exit(0)

    # Stage 2: Parse
    log.info("Stage 2: Parsing notes...")
    parsed_notes = []
    skipped = 0

    for filepath in all_notes:
        # Resume check
        if args.resume:
            cached_hash = state["imported"].get(filepath)
            if cached_hash and cached_hash == file_hash(filepath):
                skipped += 1
                continue

        note = parse_note(filepath, vault_path)
        if note is None:
            err_log.add("parse", filepath, "Failed to read file")
            continue

        # Skip empty notes
        if not note["label"] or (not note["content"] and not note["metadata"]):
            skipped += 1
            continue

        parsed_notes.append(note)

    log.info("Parsed %d notes (%d skipped)", len(parsed_notes), skipped)

    if not parsed_notes:
        log.warning("No notes to import")
        sys.exit(0)

    # Determine namespace
    namespace = args.namespace or "obsidian"

    # Stage 3: Map
    log.info("Stage 3: Mapping to MindBank graph...")

    # Build topic nodes for folders
    topic_nodes = build_topic_nodes(parsed_notes, namespace)
    log.info("Generated %d topic nodes from folders", len(topic_nodes))

    # Check for existing nodes (dedup)
    all_labels = [n["label"] for n in parsed_notes] + [t["label"] for t in topic_nodes]
    existing_ids: Dict[str, str] = {}

    if not args.dry_run:
        log.info("Checking for existing nodes...")
        for label in all_labels:
            node_id = find_existing_node(args.api_url, label, namespace)
            if node_id:
                existing_ids[label] = node_id

        if existing_ids:
            log.info("Found %d existing nodes (will skip)", len(existing_ids))

    # Filter out existing
    new_notes = [n for n in parsed_notes if n["label"] not in existing_ids]
    new_topics = [t for t in topic_nodes if t["label"] not in existing_ids]

    # Add namespace to notes
    for n in new_notes:
        n["namespace"] = resolve_namespace(namespace, n["folder"])

    log.info("New notes to create: %d", len(new_notes))
    log.info("New topics to create: %d", len(new_topics))

    # Stage 4: Upload
    log.info("Stage 4: Uploading to MindBank...")

    # Create topic nodes first
    all_nodes_to_create = new_topics + new_notes

    if not all_nodes_to_create and not args.dry_run:
        log.info("Nothing new to import")
        sys.exit(0)

    # Batch create nodes
    label_to_id = batch_create_nodes(args.api_url, all_nodes_to_create, err_log, args.dry_run)

    # Merge with existing
    label_to_id.update(existing_ids)

    # Build edges
    log.info("Building edges...")

    edges = []

    # 1. Wikilink edges
    for n in new_notes:
        src_id = label_to_id.get(n["label"])
        if not src_id:
            continue
        for link in n["wikilinks"]:
            dst_id = label_to_id.get(link)
            if dst_id and dst_id != src_id:
                edges.append({
                    "source_id": src_id,
                    "target_id": dst_id,
                    "edge_type": "relates_to",
                })

    # 2. Folder → note edges (topic contains note)
    for n in new_notes:
        src_id = label_to_id.get(n["label"])
        if not src_id or not n["folder"]:
            continue
        # Direct parent folder
        folder_name = n["folder"].strip("/").split("/")[-1]
        topic_id = label_to_id.get(folder_name)
        if topic_id and topic_id != src_id:
            edges.append({
                "source_id": topic_id,
                "target_id": src_id,
                "edge_type": "contains",
            })

    # 3. Tag-based edges
    tag_edges = build_tag_edges(new_notes, label_to_id)
    edges.extend(tag_edges)

    # Deduplicate edges
    seen_edges = set()
    unique_edges = []
    for e in edges:
        key = (e["source_id"], e["target_id"], e["edge_type"])
        if key not in seen_edges:
            seen_edges.add(key)
            unique_edges.append(e)

    log.info("Edges to create: %d (wikilinks + folders + tags)", len(unique_edges))

    # Batch create edges
    edge_count = batch_create_edges(args.api_url, unique_edges, err_log, args.dry_run)

    # Update state
    if args.resume and not args.dry_run:
        for n in parsed_notes:
            state["imported"][n["filepath"]] = n["hash"]
        save_state(args.state_file, state)

    # Summary
    log.info("")
    log.info("=== Import Summary ===")
    log.info("Notes found: %d", len(all_notes))
    log.info("Notes parsed: %d", len(parsed_notes))
    log.info("Notes created: %d", len(new_notes))
    log.info("Topics created: %d", len(new_topics))
    log.info("Edges created: %d", edge_count)
    log.info("Skipped (existing): %d", len(existing_ids))
    log.info("Errors: %d", err_log.count)
    if args.dry_run:
        log.info("Mode: DRY RUN — nothing was actually created")

    err_log.save()

    if err_log.count > 0:
        log.warning("Errors logged to %s", args.error_log)

    log.info("Done.")


if __name__ == "__main__":
    main()
