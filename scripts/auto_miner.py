#!/usr/bin/env python3
"""Auto-mine a Hermes session file into MindBank.

This script reads a Hermes session transcript, extracts knowledge,
and creates nodes in the MindBank graph database.

Usage:
    auto_miner.py <session_file> [--api URL] [--workspace WS] [--namespace NS]
"""
import argparse
import json
import os
import re
import urllib.request
import urllib.error
from pathlib import Path
from datetime import datetime

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


def extract_namespace_from_session(session_path: str) -> str | None:
    """Parse session JSON for working_directory or cwd, derive namespace."""
    try:
        with open(session_path, 'r', encoding='utf-8') as f:
            data = json.load(f)
        wd = data.get("working_directory", "").strip()
        if wd:
            return derive_namespace_from_path(wd)
        cwd = data.get("cwd", "").strip()
        if cwd:
            return derive_namespace_from_path(cwd)
    except Exception:
        pass
    return None


def extract_knowledge(content: str) -> list[dict]:
    """Extract knowledge nuggets from session content."""
    knowledge = []
    
    # Extract decisions (lines with "decided", "Decision:", etc.)
    decision_patterns = [
        r'(?i)(?:decision|decided|we will|we should|let\'s)\s*[:\-]?\s*(.+?)(?:\n|$)',
        r'(?i)(?:agreed|consensus|conclusion)\s*[:\-]?\s*(.+?)(?:\n|$)',
    ]
    for pattern in decision_patterns:
        for match in re.finditer(pattern, content):
            knowledge.append({
                "type": "decision",
                "label": match.group(1).strip()[:100],
                "content": match.group(1).strip(),
            })
    
    # Extract facts (lines with "is", "are", "was", "were" in declarative form)
    fact_patterns = [
        r'(?i)(?:fact|note|remember|important)\s*[:\-]?\s*(.+?)(?:\n|$)',
        r'(?i)(?:the|a|an)\s+\w+\s+(?:is|are|was|were)\s+(.+?)(?:\n|$)',
    ]
    for pattern in fact_patterns:
        for match in re.finditer(pattern, content):
            knowledge.append({
                "type": "fact",
                "label": match.group(1).strip()[:100],
                "content": match.group(1).strip(),
            })
    
    # Extract problems/issues
    problem_patterns = [
        r'(?i)(?:problem|issue|bug|error|fail)\s*[:\-]?\s*(.+?)(?:\n|$)',
        r'(?i)(?:broken|not working|fails|crash)\s*[:\-]?\s*(.+?)(?:\n|$)',
    ]
    for pattern in problem_patterns:
        for match in re.finditer(pattern, content):
            knowledge.append({
                "type": "problem",
                "label": match.group(1).strip()[:100],
                "content": match.group(1).strip(),
            })
    
    # Extract preferences (lines with "prefer", "like", "want")
    pref_patterns = [
        r'(?i)(?:prefer|preference|like|want|should use)\s*[:\-]?\s*(.+?)(?:\n|$)',
    ]
    for pattern in pref_patterns:
        for match in re.finditer(pattern, content):
            knowledge.append({
                "type": "preference",
                "label": match.group(1).strip()[:100],
                "content": match.group(1).strip(),
            })
    
    # Deduplicate by content similarity
    seen = set()
    unique = []
    for item in knowledge:
        key = item["content"].lower()[:50]
        if key not in seen and len(item["content"]) > 10:
            seen.add(key)
            unique.append(item)
    
    return unique[:20]  # Limit to 20 items per session


def create_node(api_base: str, node_type: str, label: str, content: str, 
                workspace: str = "default", namespace: str = "global") -> str | None:
    """Create a node in MindBank. Returns node ID or None."""
    data = json.dumps({
        "node_type": node_type,
        "label": label,
        "content": content,
        "workspace_name": workspace,
        "namespace": namespace,
    }).encode()
    
    req = urllib.request.Request(
        f"{api_base}/nodes",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            result = json.loads(resp.read())
            return result.get("id")
    except urllib.error.HTTPError as e:
        print(f"Error creating node: {e.code} {e.read().decode()[:200]}")
        return None
    except Exception as e:
        print(f"Error creating node: {e}")
        return None


def create_edge(api_base: str, source: str, target: str, edge_type: str) -> bool:
    """Create an edge between two nodes."""
    data = json.dumps({
        "source": source,
        "target": target,
        "edge_type": edge_type,
    }).encode()
    
    req = urllib.request.Request(
        f"{api_base}/edges",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return True
    except Exception as e:
        print(f"Error creating edge: {e}")
        return False


def mine_session_file(session_path: str, api_base: str = DEFAULT_API,
                      workspace: str = "default", namespace: str = "global"):
    """Mine a single session file and create nodes."""
    path = Path(session_path)
    if not path.exists():
        print(f"Session file not found: {session_path}")
        return
    
    # Derive namespace from session JSON if available
    derived_ns = extract_namespace_from_session(session_path)
    if derived_ns:
        namespace = derived_ns
        print(f"Derived namespace from session: {namespace}")
    
    # Read session content
    content = path.read_text(encoding="utf-8")
    if len(content) < 100:
        print(f"Session too short, skipping: {session_path}")
        return
    
    # Extract session name from filename or first line
    session_name = path.stem
    first_line = content.split("\n")[0].strip()
    if first_line and len(first_line) < 100:
        session_name = first_line
    
    print(f"Mining session: {session_name} (namespace: {namespace})")
    
    # Create session node
    session_id = create_node(api_base, "session", session_name, content, workspace, namespace)
    if not session_id:
        print("Failed to create session node")
        return
    
    print(f"Created session node: {session_id}")
    
    # Extract knowledge
    knowledge_items = extract_knowledge(content)
    print(f"Extracted {len(knowledge_items)} knowledge items")
    
    # Create knowledge nodes and edges
    created = 0
    for item in knowledge_items:
        node_id = create_node(api_base, item["type"], item["label"], 
                             item["content"], workspace, namespace)
        if node_id:
            create_edge(api_base, session_id, node_id, "produced")
            created += 1
    
    print(f"Created {created} knowledge nodes")
    
    # Create summary node
    summary = f"Session mined on {datetime.now().isoformat()}. "
    summary += f"Extracted {len(knowledge_items)} items ({created} created)."
    
    summary_id = create_node(api_base, "fact", f"Summary: {session_name[:50]}", 
                            summary, workspace, namespace)
    if summary_id:
        create_edge(api_base, session_id, summary_id, "produced")
    
    return session_id


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Auto-mine Hermes sessions into MindBank")
    parser.add_argument("session_file", help="Path to session file")
    parser.add_argument("--api", default=DEFAULT_API, help="MindBank API base URL")
    parser.add_argument("--workspace", default="default", help="Workspace name")
    parser.add_argument("--namespace", default="global", help="Namespace")
    
    args = parser.parse_args()
    mine_session_file(args.session_file, args.api, args.workspace, args.namespace)
