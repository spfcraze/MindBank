#!/usr/bin/env python3
"""Session file watcher - automatically mines new Hermes sessions into MindBank.

This script watches the ~/.hermes/sessions/ directory for new .json files
and automatically mines them into MindBank using auto_miner.py.

Usage:
    session_watcher.py [--daemon] [--interval SECONDS]
"""
import argparse
import json
import os
import subprocess
import sys
import time
from pathlib import Path
from datetime import datetime

SESSION_DIR = Path.home() / ".hermes" / "sessions"
WATCHED_FILE = Path.home() / ".hermes" / ".session_watcher_state.json"
MIND_BANK_API = "http://127.0.0.1:8095/api/v1"
AUTO_MINER = Path(__file__).parent / "auto_miner.py"


def load_watched_sessions() -> set:
    """Load set of already-watched session files."""
    if WATCHED_FILE.exists():
        try:
            with open(WATCHED_FILE, 'r') as f:
                return set(json.load(f))
        except:
            pass
    return set()


def save_watched_sessions(watched: set):
    """Save set of watched session files."""
    WATCHED_FILE.parent.mkdir(parents=True, exist_ok=True)
    with open(WATCHED_FILE, 'w') as f:
        json.dump(list(watched), f)


def find_session_files() -> list[Path]:
    """Find all session JSON files."""
    if not SESSION_DIR.exists():
        return []
    return sorted(SESSION_DIR.glob("*.json"), key=lambda p: p.stat().st_mtime, reverse=True)


def mine_session(session_path: Path) -> bool:
    """Mine a single session file into MindBank."""
    try:
        result = subprocess.run(
            [sys.executable, str(AUTO_MINER), str(session_path),
             "--api", MIND_BANK_API,
             "--workspace", "hermes"],
            capture_output=True,
            text=True,
            timeout=60
        )
        if result.returncode == 0:
            print(f"  ✓ Mined: {session_path.name}")
            return True
        else:
            print(f"  ✗ Failed: {session_path.name} - {result.stderr[:200]}")
            return False
    except Exception as e:
        print(f"  ✗ Error: {session_path.name} - {e}")
        return False


def watch_once():
    """Single pass: find and mine new sessions."""
    watched = load_watched_sessions()
    sessions = find_session_files()
    
    new_sessions = [s for s in sessions if str(s) not in watched]
    
    if not new_sessions:
        return 0
    
    print(f"[{datetime.now().isoformat()}] Found {len(new_sessions)} new sessions to mine")
    
    mined = 0
    for session_path in new_sessions:
        if mine_session(session_path):
            watched.add(str(session_path))
            mined += 1
    
    save_watched_sessions(watched)
    print(f"[{datetime.now().isoformat()}] Mined {mined}/{len(new_sessions)} sessions")
    return mined


def watch_daemon(interval: int = 30):
    """Run as daemon, watching for new sessions."""
    print(f"Session watcher daemon started (interval: {interval}s)")
    print(f"Watching: {SESSION_DIR}")
    print(f"API: {MIND_BANK_API}")
    
    while True:
        try:
            watch_once()
        except Exception as e:
            print(f"Error in watcher: {e}")
        
        time.sleep(interval)


def mine_all_missed():
    """Mine all sessions that haven't been mined yet."""
    watched = load_watched_sessions()
    sessions = find_session_files()
    
    missed = [s for s in sessions if str(s) not in watched]
    
    if not missed:
        print("No missed sessions to mine")
        return 0
    
    print(f"Found {len(missed)} missed sessions to mine")
    
    mined = 0
    for session_path in missed:
        if mine_session(session_path):
            watched.add(str(session_path))
            mined += 1
    
    save_watched_sessions(watched)
    print(f"Mined {mined}/{len(missed)} missed sessions")
    return mined


if __name__ == "__main__":
    parser = argparse.ArgumentParser(description="Watch for new Hermes sessions and mine into MindBank")
    parser.add_argument("--daemon", action="store_true", help="Run as daemon")
    parser.add_argument("--interval", type=int, default=30, help="Check interval in seconds (daemon mode)")
    parser.add_argument("--mine-all", action="store_true", help="Mine all missed sessions once")
    
    args = parser.parse_args()
    
    if args.mine_all:
        mine_all_missed()
    elif args.daemon:
        watch_daemon(args.interval)
    else:
        # Single run
        count = watch_once()
        sys.exit(0 if count >= 0 else 1)
