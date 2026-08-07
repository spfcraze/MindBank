#!/usr/bin/env python3
"""Batch miner for Hermes sessions into MindBank.

Processes sessions in small batches to avoid server timeouts.
Usage:
    batch_mine.py [--since-days 7] [--batch-size 10] [--workspace hermes]
"""
import argparse
import json
import os
import urllib.request
import urllib.error
from pathlib import Path
from datetime import datetime, timedelta

DEFAULT_API = "http://127.0.0.1:8095/api/v1"
SESSION_DIR = Path.home() / ".hermes" / "sessions"


def log(msg):
    ts = datetime.now().strftime("%Y-%m-%d %H:%M:%S")
    print(f"[{ts}] {msg}")


def check_api(api_base):
    """Check if MindBank API is running."""
    try:
        req = urllib.request.Request(f"{api_base}/health", method="GET")
        with urllib.request.urlopen(req, timeout=5) as resp:
            data = json.loads(resp.read())
            if data.get("status") == "ok":
                return True
    except Exception as e:
        log(f"API health check failed: {e}")
    return False


def find_recent_sessions(since_days=7):
    """Find session files modified in the last N days."""
    cutoff = datetime.now() - timedelta(days=since_days)
    sessions = []
    
    if not SESSION_DIR.exists():
        log(f"Session directory not found: {SESSION_DIR}")
        return sessions
    
    for pattern in ["*.json", "*.jsonl"]:
        for path in SESSION_DIR.glob(pattern):
            mtime = datetime.fromtimestamp(path.stat().st_mtime)
            if mtime >= cutoff:
                sessions.append((str(path), mtime))
    
    # Sort by modification time (newest first)
    sessions.sort(key=lambda x: x[1], reverse=True)
    return [s[0] for s in sessions]


def mine_batch(api_base, session_files, workspace="hermes"):
    """Mine a batch of session files via the API."""
    data = json.dumps({
        "session_files": session_files,
        "workspace": workspace,
    }).encode()
    
    req = urllib.request.Request(
        f"{api_base}/sessions/mine",
        data=data,
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    
    try:
        with urllib.request.urlopen(req, timeout=120) as resp:
            result = json.loads(resp.read())
            return result
    except urllib.error.HTTPError as e:
        error_body = e.read().decode()[:500]
        log(f"HTTP error {e.code}: {error_body}")
        return None
    except Exception as e:
        log(f"Request error: {e}")
        return None


def main():
    parser = argparse.ArgumentParser(description="Batch mine Hermes sessions into MindBank")
    parser.add_argument("--since-days", type=int, default=7, help="Only mine sessions from last N days")
    parser.add_argument("--batch-size", type=int, default=10, help="Sessions per batch")
    parser.add_argument("--workspace", default="hermes", help="Workspace name")
    parser.add_argument("--api", default=DEFAULT_API, help="MindBank API base URL")
    args = parser.parse_args()
    
    log("=== MindBank Session Batch Miner ===")
    
    # Check API
    if not check_api(args.api):
        log("WARNING: MindBank server not running on port 8095 - skipping")
        return 1
    
    log(f"MindBank API is healthy at {args.api}")
    
    # Find recent sessions
    sessions = find_recent_sessions(args.since_days)
    log(f"Found {len(sessions)} sessions modified in last {args.since_days} days")
    
    if not sessions:
        log("No sessions to mine - exiting")
        return 0
    
    # Process in batches
    total_mined = 0
    total_failed = 0
    total_processed = 0
    
    for i in range(0, len(sessions), args.batch_size):
        batch = sessions[i:i + args.batch_size]
        batch_num = i // args.batch_size + 1
        total_batches = (len(sessions) + args.batch_size - 1) // args.batch_size
        
        log(f"Processing batch {batch_num}/{total_batches} ({len(batch)} sessions)...")
        
        result = mine_batch(args.api, batch, args.workspace)
        
        if result:
            mined = result.get("mined", 0)
            failed = result.get("failed", 0)
            total = result.get("total", 0)
            total_mined += mined
            total_failed += failed
            total_processed += total
            log(f"  Batch result: {mined} mined, {failed} failed, {total} total")
            
            if result.get("errors"):
                for err in result["errors"][:3]:  # Show first 3 errors
                    log(f"  Error: {err}")
        else:
            log(f"  Batch failed - no response from API")
            total_failed += len(batch)
    
    log("=== Mining Complete ===")
    log(f"Total sessions processed: {total_processed}")
    log(f"Successfully mined: {total_mined}")
    log(f"Failed: {total_failed}")
    
    return 0


if __name__ == "__main__":
    exit(main())
