#!/usr/bin/env python3
"""Backfill namespaces for existing global nodes in MindBank.

This migration script safely re-namespaces existing "global" nodes by:
1. For session-type nodes: parsing their content as JSON to extract
   working_directory/cwd, then deriving the namespace from the path.
2. For other node types: checking edges to session nodes and inheriting
   the session's namespace.

Usage:
    python3 scripts/backfill_namespaces.py [--dry-run] [--api URL]

Safety guarantees:
    - Never deletes data.
    - Only updates the namespace field via PUT /api/v1/nodes/{id}.
    - Dry-run mode shows what would change without making any API calls.
"""
import argparse
import json
import os
import sys
import urllib.request
import urllib.error
from collections import defaultdict
from typing import Optional

DEFAULT_API = "http://127.0.0.1:8095/api/v1"


def derive_namespace_from_path(path: str) -> str:
    """Extract leaf folder name from path. Same logic as Go DeriveNamespaceFromPath."""
    path = path.strip()
    if not path or path == "/":
        return "global"
    path = path.rstrip("/")
    base = os.path.basename(path)
    if base in ("/", ".", ""):
        return "global"
    return base


def extract_namespace_from_session_content(content: str) -> Optional[str]:
    """Parse session node content as JSON to extract working_directory or cwd."""
    try:
        data = json.loads(content)
    except (json.JSONDecodeError, TypeError):
        return None

    if not isinstance(data, dict):
        return None

    wd = data.get("working_directory", "").strip()
    if wd:
        return derive_namespace_from_path(wd)
    cwd = data.get("cwd", "").strip()
    if cwd:
        return derive_namespace_from_path(cwd)
    return None


def api_get(api_base: str, path: str) -> dict:
    """Make a GET request and return parsed JSON."""
    url = f"{api_base}{path}"
    req = urllib.request.Request(url, method="GET")
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode()[:500]
        raise RuntimeError(f"GET {url} failed: {e.code} {body}") from e
    except Exception as e:
        raise RuntimeError(f"GET {url} failed: {e}") from e


def api_put(api_base: str, path: str, payload: dict) -> dict:
    """Make a PUT request with JSON body and return parsed JSON."""
    url = f"{api_base}{path}"
    data = json.dumps(payload).encode("utf-8")
    req = urllib.request.Request(
        url,
        data=data,
        headers={"Content-Type": "application/json"},
        method="PUT",
    )
    try:
        with urllib.request.urlopen(req, timeout=30) as resp:
            return json.loads(resp.read())
    except urllib.error.HTTPError as e:
        body = e.read().decode()[:500]
        raise RuntimeError(f"PUT {url} failed: {e.code} {body}") from e
    except Exception as e:
        raise RuntimeError(f"PUT {url} failed: {e}") from e


def fetch_all_global_nodes(api_base: str) -> list[dict]:
    """Fetch all nodes with namespace=global, handling pagination."""
    nodes = []
    offset = 0
    limit = 200
    while True:
        batch = api_get(api_base, f"/nodes?namespace=global&limit={limit}&offset={offset}")
        if not isinstance(batch, list):
            raise RuntimeError(f"Unexpected response from /nodes: {type(batch)}")
        if not batch:
            break
        nodes.extend(batch)
        if len(batch) < limit:
            break
        offset += limit
    return nodes


def fetch_node_neighbors(api_base: str, node_id: str) -> list[dict]:
    """Fetch 1-hop neighbors for a node via the neighbors endpoint."""
    try:
        result = api_get(api_base, f"/nodes/{node_id}/neighbors")
        if isinstance(result, list):
            return result
        if isinstance(result, dict) and "error" in result:
            print(f"  [WARN] Neighbors API error for {node_id}: {result['error']}")
            return []
        return []
    except Exception as e:
        print(f"  [WARN] Failed to fetch neighbors for {node_id}: {e}")
        return []


def build_session_namespace_map(nodes: list[dict]) -> dict[str, str]:
    """Build a map of session node ID -> derived namespace from content."""
    session_ns = {}
    for node in nodes:
        if node.get("node_type") == "session":
            ns = extract_namespace_from_session_content(node.get("content", ""))
            if ns and ns != "global":
                session_ns[node["id"]] = ns
    return session_ns


def infer_namespace_for_node(
    node: dict,
    session_ns_map: dict[str, str],
    neighbor_cache: dict[str, list[dict]],
    api_base: str,
) -> Optional[str]:
    """Infer namespace for a non-session node by looking at its neighbors."""
    node_id = node["id"]

    # Use cached neighbors if available
    neighbors = neighbor_cache.get(node_id)
    if neighbors is None:
        neighbors = fetch_node_neighbors(api_base, node_id)
        neighbor_cache[node_id] = neighbors

    # Collect namespaces from connected session nodes
    candidate_namespaces = []
    for nb in neighbors:
        nb_id = nb.get("id")
        nb_type = nb.get("node_type")
        if nb_type == "session" and nb_id in session_ns_map:
            candidate_namespaces.append(session_ns_map[nb_id])

    if not candidate_namespaces:
        return None

    # If all session neighbors agree, use that namespace
    if len(set(candidate_namespaces)) == 1:
        return candidate_namespaces[0]

    # If there are multiple candidates, pick the most common one
    freq = defaultdict(int)
    for ns in candidate_namespaces:
        freq[ns] += 1
    best_ns = max(freq, key=freq.get)
    return best_ns


def backfill_namespaces(api_base: str, dry_run: bool = True) -> dict:
    """Main backfill logic. Returns summary statistics."""
    print(f"Fetching all global nodes from {api_base}...")
    nodes = fetch_all_global_nodes(api_base)
    total = len(nodes)
    print(f"Found {total} global node(s).")

    if total == 0:
        return {"total": 0, "changed": 0, "unchanged": 0, "errors": 0}

    # Build session namespace map first
    session_ns_map = build_session_namespace_map(nodes)
    print(f"Derived namespaces for {len(session_ns_map)} session node(s).")

    # Track stats
    stats = {"total": total, "changed": 0, "unchanged": 0, "errors": 0, "details": []}

    # Cache for neighbor lookups
    neighbor_cache: dict[str, list[dict]] = {}

    for node in nodes:
        node_id = node.get("id", "?")
        node_type = node.get("node_type", "?")
        current_ns = node.get("namespace", "global")
        label = node.get("label", "?")[:60]

        inferred_ns: Optional[str] = None
        source = ""

        if node_type == "session":
            inferred_ns = session_ns_map.get(node_id)
            if inferred_ns:
                source = "session_content"
        else:
            inferred_ns = infer_namespace_for_node(node, session_ns_map, neighbor_cache, api_base)
            if inferred_ns:
                source = "edge_to_session"

        if not inferred_ns or inferred_ns == current_ns:
            stats["unchanged"] += 1
            continue

        stats["changed"] += 1
        stats["details"].append({
            "id": node_id,
            "label": label,
            "type": node_type,
            "old_ns": current_ns,
            "new_ns": inferred_ns,
            "source": source,
        })

        if dry_run:
            print(f"  [DRY-RUN] Would update {node_id} ({node_type}): '{label}'")
            print(f"            {current_ns} -> {inferred_ns}  (source: {source})")
        else:
            try:
                result = api_put(api_base, f"/nodes/{node_id}", {"namespace": inferred_ns})
                print(f"  [UPDATED] {node_id} ({node_type}): '{label}' -> {inferred_ns}")
                # Verify the response
                returned_ns = result.get("namespace", "?")
                if returned_ns != inferred_ns:
                    print(f"  [WARN] Namespace mismatch in response: expected {inferred_ns}, got {returned_ns}")
            except Exception as e:
                stats["errors"] += 1
                print(f"  [ERROR] Failed to update {node_id}: {e}")

    return stats


def main():
    parser = argparse.ArgumentParser(
        description="Backfill namespaces for existing global MindBank nodes"
    )
    parser.add_argument(
        "--api",
        default=DEFAULT_API,
        help=f"MindBank API base URL (default: {DEFAULT_API})",
    )
    parser.add_argument(
        "--dry-run",
        action="store_true",
        default=True,
        help="Show what would change without updating (default: true)",
    )
    parser.add_argument(
        "--no-dry-run",
        action="store_true",
        help="Actually perform the updates (disables dry-run)",
    )
    args = parser.parse_args()

    dry_run = args.dry_run and not args.no_dry_run

    print("=" * 60)
    print("MindBank Namespace Backfill Script")
    print("=" * 60)
    print(f"API base:   {args.api}")
    print(f"Mode:       {'DRY-RUN (no changes)' if dry_run else 'LIVE (will update)'}")
    print("-" * 60)

    try:
        stats = backfill_namespaces(args.api, dry_run=dry_run)
    except Exception as e:
        print(f"\n[FATAL] {e}")
        sys.exit(1)

    print("\n" + "=" * 60)
    print("Summary")
    print("=" * 60)
    print(f"Total global nodes scanned: {stats['total']}")
    print(f"Nodes that would change:    {stats['changed']}")
    print(f"Nodes unchanged:            {stats['unchanged']}")
    if not dry_run:
        print(f"Errors during update:       {stats.get('errors', 0)}")

    if dry_run and stats["changed"] > 0:
        print("\nRun with --no-dry-run to apply these changes.")

    # Non-zero exit if there were errors in live mode
    if not dry_run and stats.get("errors", 0) > 0:
        sys.exit(2)

    sys.exit(0)


if __name__ == "__main__":
    main()
