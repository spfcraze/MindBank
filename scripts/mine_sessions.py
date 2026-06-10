#!/usr/bin/env python3
"""
MindBank Session Fact Extraction
Extracts knowledge nodes from session content using NLP patterns.
Uses Praxis methodology: extract facts -> classify -> create nodes.
"""

import psycopg2
import hashlib
import re
from datetime import datetime, timedelta
from uuid import uuid4

DB_CONFIG = {
    'host': '172.18.0.2',
    'port': 5432,
    'database': 'mindbank',
    'user': 'mindbank',
    'password': 'mindbank'
}


def connect():
    return psycopg2.connect(**DB_CONFIG)


def extract_facts_from_session(session_id, title, session_data, namespace):
    """Extract structured facts from session data using regex patterns."""
    facts = []
    
    # Extract content from session_data dict
    content = ""
    if isinstance(session_data, dict):
        content = session_data.get('content', '')
        if isinstance(content, str):
            try:
                # Try to parse as JSON
                import json
                parsed = json.loads(content)
                if isinstance(parsed, dict):
                    # Extract from messages or content fields
                    messages = parsed.get('messages', [])
                    if messages:
                        content = ' '.join([str(m.get('content', '')) for m in messages if isinstance(m, dict)])
                    else:
                        content = parsed.get('content', '') or str(parsed)
            except:
                pass  # Keep content as-is if not JSON
    elif isinstance(session_data, str):
        content = session_data
    
    if not content and title:
        # Extract from title if no content
        facts.append({
            'label': title[:100],
            'node_type': 'fact',
            'content': title,
            'summary': f"Session: {title[:80]}",
        })
        return facts
    
    if not content:
        return facts
    
    # Pattern 1: Extract decisions (lines with "decided", "decision", "chose")
    decision_patterns = [
        r'(?i)(?:decided?|decision|chose|chosen|opted|selected)\s+(?:to\s+)?(.+?)(?:\.|$|\n)',
        r'(?i)(?:we|i)\s+(?:will|shall|going to)\s+(.+?)(?:\.|$|\n)',
        r'(?i)(?:conclusion|concluded)\s+(?:is\s+)?(?:that\s+)?(.+?)(?:\.|$|\n)',
    ]
    for pattern in decision_patterns:
        for match in re.finditer(pattern, content):
            text = match.group(1).strip()
            if len(text) > 10:
                facts.append({
                    'label': f"Decision: {text[:80]}",
                    'node_type': 'decision',
                    'content': text,
                    'summary': f"Decision from session: {title[:50]}",
                })
    
    # Pattern 2: Extract advice (lines with "should", "recommend", "best practice")
    advice_patterns = [
        r'(?i)(?:should|must|recommend|best practice|advice|suggestion)\s+(?:is\s+to\s+)?(.+?)(?:\.|$|\n)',
        r'(?i)(?:it is|it\'s)\s+(?:recommended|advised|suggested)\s+(?:to\s+)?(.+?)(?:\.|$|\n)',
    ]
    for pattern in advice_patterns:
        for match in re.finditer(pattern, content):
            text = match.group(1).strip()
            if len(text) > 10:
                facts.append({
                    'label': f"Advice: {text[:80]}",
                    'node_type': 'advice',
                    'content': text,
                    'summary': f"Advice from session: {title[:50]}",
                })
    
    # Pattern 3: Extract problems/issues
    problem_patterns = [
        r'(?i)(?:problem|issue|bug|error|failure)\s+(?:is\s+)?(?:that\s+)?(.+?)(?:\.|$|\n)',
        r'(?i)(?:fix|solve|resolve)\s+(?:the\s+)?(.+?)(?:\.|$|\n)',
    ]
    for pattern in problem_patterns:
        for match in re.finditer(pattern, content):
            text = match.group(1).strip()
            if len(text) > 10:
                facts.append({
                    'label': f"Problem: {text[:80]}",
                    'node_type': 'problem',
                    'content': text,
                    'summary': f"Problem from session: {title[:50]}",
                })
    
    # Pattern 4: Extract concepts/definitions
    concept_patterns = [
        r'(?i)(?:concept|definition|means|refers to)\s+(?:is\s+)?(.+?)(?:\.|$|\n)',
    ]
    for pattern in concept_patterns:
        for match in re.finditer(pattern, content):
            text = match.group(1).strip()
            if len(text) > 10:
                facts.append({
                    'label': f"Concept: {text[:80]}",
                    'node_type': 'concept',
                    'content': text,
                    'summary': f"Concept from session: {title[:50]}",
                })
    
    # If no patterns matched, extract from title and first few sentences
    if not facts:
        sentences = re.split(r'[.!?]+', content)
        for sent in sentences[:2]:  # Top 2 sentences
            sent = sent.strip()
            if len(sent) > 20 and len(sent) < 500:
                facts.append({
                    'label': title[:100] if title else sent[:100],
                    'node_type': 'fact',
                    'content': sent,
                    'summary': f"Fact from session: {title[:50]}",
                })
    
    return facts


def create_node_from_fact(conn, fact, session_id, namespace, workspace='hermes'):
    """Create a node from extracted fact with deduplication."""
    cur = conn.cursor()
    
    node_id = str(uuid4())
    now = datetime.utcnow()
    valid_to = now + timedelta(days=365)
    
    # Compute hash for deduplication
    content = fact['content']
    content_hash = hashlib.sha256(content.encode()).hexdigest()
    
    # Check for duplicates
    cur.execute("""
        SELECT id FROM nodes 
        WHERE content_hash = %s AND valid_to IS NULL
        LIMIT 1
    """, (content_hash,))
    
    if cur.fetchone():
        cur.close()
        return None  # Duplicate
    
    # Insert node
    cur.execute("""
        INSERT INTO nodes (
            id, label, node_type, content, summary, namespace, 
            workspace_name, source_type, source_id, content_hash,
            created_at, valid_to, importance
        ) VALUES (%s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s, %s)
    """, (
        node_id, fact['label'], fact['node_type'], content,
        fact['summary'], namespace, workspace, 'session',
        str(session_id), content_hash, now, valid_to, 0.5
    ))
    
    conn.commit()
    cur.close()
    return node_id


def mine_sessions(batch_size=100):
    """Mine unprocessed sessions for facts."""
    conn = connect()
    
    # Get sessions that haven't been mined
    cur = conn.cursor()
    cur.execute("""
        SELECT s.id, s.name, s.session_data, s.namespace
        FROM sessions s
        LEFT JOIN nodes n ON n.source_id = s.id AND n.source_type = 'session'
        WHERE n.id IS NULL
        LIMIT %s
    """, (batch_size,))
    
    sessions = cur.fetchall()
    cur.close()
    
    total_facts = 0
    total_nodes = 0
    
    for session_id, title, content, namespace in sessions:
        facts = extract_facts_from_session(session_id, title, content, namespace)
        
        for fact in facts:
            node_id = create_node_from_fact(conn, fact, session_id, namespace)
            if node_id:
                total_nodes += 1
        
        total_facts += len(facts)
    
    conn.close()
    
    return total_facts, total_nodes, len(sessions)


def mine_all_sessions():
    """Mine all unprocessed sessions in batches."""
    total_facts = 0
    total_nodes = 0
    total_sessions = 0
    batch = 0
    
    while True:
        batch += 1
        facts, nodes, sessions = mine_sessions(batch_size=100)
        
        if sessions == 0:
            break
        
        total_facts += facts
        total_nodes += nodes
        total_sessions += sessions
        
        print(f"Batch {batch}: Processed {sessions} sessions, extracted {facts} facts, created {nodes} nodes")
    
    print(f"\nTotal: Processed {total_sessions} sessions, extracted {total_facts} facts, created {total_nodes} nodes")
    return total_facts, total_nodes, total_sessions


if __name__ == '__main__':
    print("MindBank Session Fact Extraction")
    print("=" * 50)
    
    # Check available sessions
    conn = connect()
    cur = conn.cursor()
    cur.execute("SELECT COUNT(*) FROM sessions")
    total = cur.fetchone()[0]
    cur.execute("""
        SELECT COUNT(*) FROM sessions s
        LEFT JOIN nodes n ON n.source_id = s.id::text AND n.source_type = 'session'
        WHERE n.id IS NULL
    """)
    unmined = cur.fetchone()[0]
    cur.close()
    conn.close()
    
    print(f"Total sessions: {total}")
    print(f"Unmined sessions: {unmined}")
    
    if unmined > 0:
        print("\nStarting extraction...")
        mine_all_sessions()
    else:
        print("\nNo sessions to mine.")
