#!/usr/bin/env python3
"""
MindBank Session Fact Extraction - Fast Batch Version
Extracts knowledge nodes from session content using NLP patterns.
"""

import psycopg2
import hashlib
import json
import re
from datetime import datetime, timezone, timedelta
from uuid import uuid4

DB_CONFIG = {
    'host': '172.18.0.2',
    'port': 5432,
    'database': 'mindbank',
    'user': 'mindbank',
    'password': 'mindbank'
}



MIN_CONTENT_LENGTH = 50  # Minimum chars for meaningful search

def connect():
    return psycopg2.connect(**DB_CONFIG)


def extract_content_from_session_data(session_data):
    """Extract text content from session_data JSONB."""
    if not isinstance(session_data, dict):
        return ""
    
    content = session_data.get('content', '')
    if not isinstance(content, str):
        return ""
    
    try:
        parsed = json.loads(content)
        if isinstance(parsed, dict):
            messages = parsed.get('messages', [])
            if messages:
                texts = []
                for m in messages:
                    if isinstance(m, dict):
                        msg_content = m.get('content', '')
                        if msg_content and isinstance(msg_content, str):
                            texts.append(msg_content)
                return '\n'.join(texts)
            return parsed.get('content', '') or str(parsed)
    except:
        pass
    
    return content


def extract_facts(session_id, name, session_data, namespace):
    """Extract facts from session content."""
    facts = []
    content = extract_content_from_session_data(session_data)
    
    if not content and name:
        if len(name) < MIN_CONTENT_LENGTH:
            return facts  # Skip very short names
        
        return [{
            'label': name[:100],
            'node_type': 'fact',
            'content': name,
            'summary': f"Session: {name[:80]}",
        }]
    
    if not content:
        return facts
    
    # Extract decisions
    patterns = [
        (r'(?i)(?:decided?|decision|chose|chosen|opted|selected)\s+(?:to\s+)?(.+?)(?:\.|$|\n)', 'decision'),
        (r'(?i)(?:we|i)\s+(?:will|shall|going to)\s+(.+?)(?:\.|$|\n)', 'decision'),
        (r'(?i)(?:should|must|recommend|best practice|advice)\s+(?:is\s+to\s+)?(.+?)(?:\.|$|\n)', 'advice'),
        (r'(?i)(?:problem|issue|bug|error|failure)\s+(?:is\s+)?(?:that\s+)?(.+?)(?:\.|$|\n)', 'problem'),
    ]
    
    for pattern, node_type in patterns:
        for match in re.finditer(pattern, content):
            text = match.group(1).strip()
            if len(text) > 10:
                facts.append({
                    'label': f"{node_type.capitalize()}: {text[:80]}",
                    'node_type': node_type,
                    'content': text,
                    'summary': f"{node_type.capitalize()} from session: {name[:50]}",
                })
    
    # If no patterns matched, use first sentences
    if not facts:
        sentences = re.split(r'[.!?]+', content)
        for sent in sentences[:2]:
            sent = sent.strip()
            if len(sent) > 20 and len(sent) < 500:
                facts.append({
                    'label': name[:100] if name else sent[:100],
                    'node_type': 'fact',
                    'content': sent,
                    'summary': f"Fact from session: {name[:50]}",
                })
    
    return facts


def create_node(conn, fact, session_id, namespace):
    """Create a node from extracted fact."""
    cur = conn.cursor()
    node_id = str(uuid4())
    now = datetime.now(timezone.utc)
    valid_to = now + timedelta(days=365)
    content = fact['content']
    content_hash = hashlib.sha256(content.encode()).hexdigest()
    
    # Check for duplicates
    cur.execute("SELECT id FROM nodes WHERE content_hash = %s LIMIT 1", (content_hash,))
    if cur.fetchone():
        cur.close()
        return None
    
    cur.execute("""
        INSERT INTO nodes (id, label, node_type, content, summary, namespace,
            workspace_name, source_type, source_id, content_hash,
            created_at, valid_to, importance)
        VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
    """, (
        node_id, fact['label'], fact['node_type'], content,
        fact['summary'], namespace, 'hermes', 'session',
        str(session_id), content_hash, now, valid_to, 0.5
    ))
    
    conn.commit()
    cur.close()
    return node_id


def mine_batch(batch_size=50):
    """Mine a batch of sessions."""
    conn = connect()
    cur = conn.cursor()
    cur.execute("""
        SELECT s.id, s.name, s.session_data, s.namespace
        FROM sessions s
        LEFT JOIN nodes n ON n.source_id = s.id AND n.source_type = 'session'
        WHERE n.id IS NULL
        LIMIT %s
    """, (batch_size,))
    
    rows = cur.fetchall()
    cur.close()
    
    total_facts = 0
    total_nodes = 0
    
    for session_id, name, session_data, namespace in rows:
        facts = extract_facts(session_id, name, session_data, namespace)
        # Filter out short facts
    facts = [f for f in facts if len(f['content']) >= MIN_CONTENT_LENGTH]
    
    for fact in facts:
            if create_node(conn, fact, session_id, namespace):
                total_nodes += 1
        total_facts += len(facts)
    
    conn.close()
    return total_facts, total_nodes, len(rows)


def mine_all():
    """Mine all sessions."""
    total_facts = 0
    total_nodes = 0
    total_sessions = 0
    batch = 0
    
    while True:
        batch += 1
        facts, nodes, sessions = mine_batch(batch_size=50)
        if sessions == 0:
            break
        total_facts += facts
        total_nodes += nodes
        total_sessions += sessions
        print(f"Batch {batch}: {sessions} sessions -> {facts} facts -> {nodes} nodes")
    
    print(f"\nTotal: {total_sessions} sessions -> {total_facts} facts -> {total_nodes} nodes")
    return total_facts, total_nodes, total_sessions


if __name__ == '__main__':
    print("MindBank Session Fact Extraction")
    print("=" * 50)
    
    conn = connect()
    cur = conn.cursor()
    cur.execute("SELECT COUNT(*) FROM sessions")
    total = cur.fetchone()[0]
    cur.execute("""
        SELECT COUNT(*) FROM sessions s
        LEFT JOIN nodes n ON n.source_id = s.id AND n.source_type = 'session'
        WHERE n.id IS NULL
    """)
    unmined = cur.fetchone()[0]
    cur.close()
    conn.close()
    
    print(f"Total sessions: {total}")
    print(f"Unmined: {unmined}")
    
    if unmined > 0:
        print("\nStarting extraction...")
        mine_all()
    else:
        print("\nNothing to mine.")
