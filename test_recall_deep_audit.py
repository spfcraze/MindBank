#!/usr/bin/env python3
"""
MindBank Deep Recall Audit
Uses Praxis diagnostic reasoning + Superpowers systematic debugging
Goal: Achieve 100% cross-session recall by identifying root causes of gaps
"""

import psycopg2
import json
import time
import urllib.request
import urllib.parse
from datetime import datetime, timezone
from collections import Counter

DB_CONFIG = {
    'host': '172.18.0.2',
    'port': 5432,
    'database': 'mindbank',
    'user': 'mindbank',
    'password': 'mindbank'
}

API_BASE = "http://localhost:8095"
API_V1 = f"{API_BASE}/api/v1"


def db_connect():
    return psycopg2.connect(**DB_CONFIG)


def api_get(path):
    url = f"{API_V1}{path}"
    try:
        req = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except Exception as e:
        return -1, {"error": str(e)}


def api_post(path, data):
    url = f"{API_V1}{path}"
    try:
        req = urllib.request.Request(
            url, data=json.dumps(data).encode(),
            headers={"Content-Type": "application/json"},
            method="POST"
        )
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except Exception as e:
        return -1, {"error": str(e)}


# =============================================================================
# PHASE 1: ROOT CAUSE INVESTIGATION
# =============================================================================

class RecallAuditor:
    """Deep audit of MindBank recall system using Praxis methodology."""
    
    def __init__(self):
        self.findings = []
        self.conn = db_connect()
        self.cur = self.conn.cursor()
    
    def log(self, category, severity, message, detail=""):
        self.findings.append({
            "category": category,
            "severity": severity,
            "message": message,
            "detail": detail,
            "timestamp": datetime.now(timezone.utc).isoformat()
        })
        icon = {"CRITICAL": "🔴", "HIGH": "🟠", "MEDIUM": "🟡", "LOW": "🟢", "INFO": "🔵"}[severity]
        print(f"{icon} [{severity}] {message}")
        if detail:
            print(f"   {detail}")
    
    # -------------------------------------------------------------------------
    # HYPOTHESIS 1: Embedding pipeline not processing new nodes
    # -------------------------------------------------------------------------
    def test_embedding_pipeline(self):
        print("\n" + "="*60)
        print("HYPOTHESIS 1: Embedding Pipeline Gap")
        print("="*60)
        
        # Check embedding queue
        self.cur.execute("SELECT COUNT(*) FROM embedding_queue WHERE status = 'pending'")
        pending = self.cur.fetchone()[0]
        
        self.cur.execute("SELECT COUNT(*) FROM embedding_queue WHERE status = 'done'")
        done = self.cur.fetchone()[0]
        
        self.cur.execute("SELECT COUNT(*) FROM embedding_queue WHERE status = 'failed'")
        failed = self.cur.fetchone()[0]
        
        self.cur.execute("SELECT COUNT(*) FROM node_embeddings")
        embeddings = self.cur.fetchone()[0]
        
        self.cur.execute("SELECT COUNT(*) FROM nodes WHERE valid_to IS NULL")
        total_nodes = self.cur.fetchone()[0]
        
        print(f"\nEmbedding Stats:")
        print(f"  Pending:   {pending}")
        print(f"  Done:      {done}")
        print(f"  Failed:    {failed}")
        print(f"  Total embeddings: {embeddings}")
        print(f"  Total nodes:      {total_nodes}")
        print(f"  Coverage:         {embeddings/max(total_nodes,1)*100:.1f}%")
        
        if pending > 100:
            self.log("EMBEDDING", "HIGH", 
                     f"{pending} embeddings pending - pipeline bottleneck",
                     "New nodes won't be searchable via vector similarity")
        
        if failed > 0:
            self.cur.execute("SELECT last_error, COUNT(*) FROM embedding_queue WHERE status='failed' GROUP BY last_error LIMIT 5")
            for row in self.cur.fetchall():
                self.log("EMBEDDING", "MEDIUM", 
                         f"Embedding failures: {row[1]} with error: {row[0][:100] if row[0] else 'None'}",
                         "These nodes won't be found by semantic search")
        
        if embeddings < total_nodes * 0.9:
            self.log("EMBEDDING", "HIGH",
                     f"Only {embeddings}/{total_nodes} nodes have embeddings",
                     "Vector search will miss {total_nodes - embeddings} nodes")
        else:
            self.log("EMBEDDING", "INFO",
                     f"Embedding coverage: {embeddings}/{total_nodes} ({embeddings/max(total_nodes,1)*100:.1f}%)")
        
        return pending, done, failed, embeddings, total_nodes
    
    # -------------------------------------------------------------------------
    # HYPOTHESIS 2: Search ranking algorithm issues
    # -------------------------------------------------------------------------
    def test_search_ranking(self):
        print("\n" + "="*60)
        print("HYPOTHESIS 2: Search Ranking Algorithm")
        print("="*60)
        
        # Create test nodes with known content
        test_nodes = []
        test_contents = [
            ("Deployment Best Practice", "advice", "Always run tests before deploying. Use blue-green deployment."),
            ("Rollback Strategy", "advice", "Rollback if error rate exceeds 0.1% after deployment."),
            ("Monitoring Setup", "fact", "Monitor error rates for 30 minutes after deployment."),
        ]
        
        for label, node_type, content in test_contents:
            status, data = api_post("/nodes/", {
                "label": label,
                "node_type": node_type,
                "content": content,
                "summary": f"Test: {label}",
                "namespace": "recall_test",
                "workspace_name": "hermes"
            })
            if status == 201:
                test_nodes.append(data["id"])
        
        print(f"\nCreated {len(test_nodes)} test nodes")
        
        # Wait for embeddings
        print("Waiting 5s for embeddings...")
        time.sleep(5)
        
        # Test various queries
        queries = [
            ("deployment", ["Deployment Best Practice"]),
            ("rollback strategy", ["Rollback Strategy"]),
            ("monitoring error rates", ["Monitoring Setup"]),
            ("blue green deployment", ["Deployment Best Practice"]),
            ("how to deploy safely", ["Deployment Best Practice", "Rollback Strategy"]),
        ]
        
        results = []
        for query, expected_labels in queries:
            url = f"{API_V1}/search?q={urllib.parse.quote(query)}&limit=10&namespace=recall_test"
            try:
                req = urllib.request.Request(url, method="GET")
                with urllib.request.urlopen(req, timeout=10) as resp:
                    data = json.loads(resp.read().decode())
                    search_results = data if isinstance(data, list) else data.get("results", [])
                    
                    found_labels = [r.get("label", "") for r in search_results[:5]]
                    found_expected = any(el in fl for el in expected_labels for fl in found_labels)
                    
                    results.append({
                        "query": query,
                        "expected": expected_labels,
                        "found": found_labels[:3],
                        "success": found_expected
                    })
                    
                    status_icon = "✅" if found_expected else "❌"
                    print(f"{status_icon} '{query}' -> {found_labels[:3]}")
            except Exception as e:
                results.append({"query": query, "error": str(e), "success": False})
                print(f"❌ '{query}' -> ERROR: {e}")
        
        success_rate = sum(1 for r in results if r.get("success")) / len(results) * 100
        print(f"\nSearch success rate: {success_rate:.0f}%")
        
        if success_rate < 80:
            self.log("SEARCH", "HIGH",
                     f"Search success rate only {success_rate:.0f}%",
                     "Ranking algorithm or embedding quality issue")
        
        # Cleanup
        for nid in test_nodes:
            api_post(f"/nodes/{nid}", {"valid_to": datetime.now(timezone.utc).isoformat()})
        
        return results
    
    # -------------------------------------------------------------------------
    # HYPOTHESIS 3: Content quality / truncation issues
    # -------------------------------------------------------------------------
    def test_content_quality(self):
        print("\n" + "="*60)
        print("HYPOTHESIS 3: Content Quality / Truncation")
        print("="*60)
        
        # Check for truncated content
        self.cur.execute("""
            SELECT label, LENGTH(content), LENGTH(summary), node_type
            FROM nodes
            WHERE source_type = 'session'
            ORDER BY LENGTH(content) DESC
            LIMIT 10
        """)
        
        print("\nTop 10 longest session-extracted contents:")
        for row in self.cur.fetchall():
            print(f"  {row[0][:50]}... | content: {row[1]} chars | summary: {row[2]} chars | type: {row[3]}")
        
        # Check for very short/empty content
        self.cur.execute("""
            SELECT COUNT(*) FROM nodes
            WHERE source_type = 'session' AND (content IS NULL OR LENGTH(content) < 20)
        """)
        short_count = self.cur.fetchone()[0]
        
        if short_count > 0:
            self.log("CONTENT", "MEDIUM",
                     f"{short_count} session nodes have very short content (<20 chars)",
                     "These won't be meaningfully searchable")
        
        # Check embedding input length
        self.cur.execute("""
            SELECT AVG(LENGTH(content)) FROM nodes WHERE valid_to IS NULL
        """)
        avg_len = self.cur.fetchone()[0] or 0
        print(f"\nAverage content length: {avg_len:.0f} chars")
        
        if avg_len > 1500:
            self.log("CONTENT", "MEDIUM",
                     f"Average content length {avg_len:.0f} chars exceeds 1500 char truncation",
                     "Embedding may lose semantic information from truncation")
    
    # -------------------------------------------------------------------------
    # HYPOTHESIS 4: Namespace / workspace filtering issues
    # -------------------------------------------------------------------------
    def test_namespace_filtering(self):
        print("\n" + "="*60)
        print("HYPOTHESIS 4: Namespace/Workspace Filtering")
        print("="*60)
        
        self.cur.execute("SELECT namespace, COUNT(*) FROM nodes WHERE valid_to IS NULL GROUP BY namespace ORDER BY COUNT(*) DESC")
        namespaces = self.cur.fetchall()
        
        print("\nNamespace distribution:")
        for ns, count in namespaces[:10]:
            print(f"  {ns}: {count}")
        
        # Test if namespace filter blocks recall
        test_ns = "hermes_test"
        
        # Create node in test namespace
        status, data = api_post("/nodes/", {
            "label": "Namespace Test Node",
            "node_type": "fact",
            "content": "This is a test node for namespace filtering verification.",
            "namespace": test_ns,
            "workspace_name": "hermes"
        })
        
        if status == 201:
            node_id = data["id"]
            time.sleep(2)
            
            # Search WITH namespace
            url_with = f"{API_V1}/search?q=namespace+test&limit=5&namespace={test_ns}"
            req = urllib.request.Request(url_with, method="GET")
            with urllib.request.urlopen(req, timeout=10) as resp:
                data_with = json.loads(resp.read().decode())
                results_with = data_with if isinstance(data_with, list) else data_with.get("results", [])
            
            # Search WITHOUT namespace
            url_without = f"{API_V1}/search?q=namespace+test&limit=5"
            req = urllib.request.Request(url_without, method="GET")
            with urllib.request.urlopen(req, timeout=10) as resp:
                data_without = json.loads(resp.read().decode())
                results_without = data_without if isinstance(data_without, list) else data_without.get("results", [])
            
            found_with = any(node_id == r.get("node_id") for r in results_with)
            found_without = any(node_id == r.get("node_id") for r in results_without)
            
            print(f"\nNamespace filter test:")
            print(f"  With namespace '{test_ns}': {'✅ FOUND' if found_with else '❌ NOT FOUND'}")
            print(f"  Without namespace: {'✅ FOUND' if found_without else '❌ NOT FOUND'}")
            
            if not found_with:
                self.log("NAMESPACE", "HIGH",
                         "Namespace filter blocks valid results",
                         "Nodes not found when searching within their namespace")
            
            if not found_without:
                self.log("NAMESPACE", "MEDIUM",
                         "Cross-namespace search misses nodes",
                         "Nodes only found when namespace exactly matches")
            
            # Cleanup
            api_post(f"/nodes/{node_id}", {"valid_to": datetime.now(timezone.utc).isoformat()})
    
    # -------------------------------------------------------------------------
    # HYPOTHESIS 5: Epistemic label / classification gaps
    # -------------------------------------------------------------------------
    def test_epistemic_labels(self):
        print("\n" + "="*60)
        print("HYPOTHESIS 5: Epistemic Label Classification")
        print("="*60)
        
        self.cur.execute("""
            SELECT epistemic_label, COUNT(*) 
            FROM nodes 
            WHERE valid_to IS NULL 
            GROUP BY epistemic_label 
            ORDER BY COUNT(*) DESC
        """)
        
        labels = self.cur.fetchall()
        print("\nEpistemic label distribution:")
        for label, count in labels:
            print(f"  {label or 'NULL'}: {count}")
        
        unknown_count = next((c for l, c in labels if l == 'unknown' or l is None), 0)
        total = sum(c for _, c in labels)
        
        if unknown_count / max(total, 1) > 0.8:
            self.log("EPISTEMIC", "HIGH",
                     f"{unknown_count}/{total} nodes have 'unknown' epistemic label",
                     "Auto-classification not working - all nodes treated equally")
        
        # Check if epistemic labels affect search
        self.cur.execute("""
            SELECT label, epistemic_label, content 
            FROM nodes 
            WHERE epistemic_label != 'unknown' AND epistemic_label IS NOT NULL
            LIMIT 5
        """)
        
        print("\nNodes with non-unknown epistemic labels:")
        for row in self.cur.fetchall():
            print(f"  {row[0][:50]}... | label: {row[1]}")
    
    # -------------------------------------------------------------------------
    # DISCRIMINATING TEST: Create comprehensive test matrix
    # -------------------------------------------------------------------------
    def run_discriminating_test(self):
        print("\n" + "="*60)
        print("DISCRIMINATING TEST: Cross-Session Recall Matrix")
        print("="*60)
        
        # Create test memories with different characteristics
        test_cases = [
            {
                "name": "Simple Fact",
                "label": "Python List Comprehension",
                "content": "Python list comprehensions provide a concise way to create lists. Example: [x*2 for x in range(10)]",
                "node_type": "fact",
                "queries": ["python list comprehension", "how to create lists in python", "[x*2 for x in range]"]
            },
            {
                "name": "Decision",
                "label": "Use PostgreSQL for Production",
                "content": "Decision: Use PostgreSQL instead of SQLite for production deployments due to concurrency requirements and reliability needs.",
                "node_type": "decision",
                "queries": ["database choice", "postgresql vs sqlite", "production database"]
            },
            {
                "name": "Advice",
                "label": "Always Use Transactions",
                "content": "Advice: Always wrap database writes in transactions to ensure data consistency. Use BEGIN/COMMIT blocks.",
                "node_type": "advice",
                "queries": ["database transactions", "ensure data consistency", "BEGIN COMMIT"]
            },
            {
                "name": "Problem",
                "label": "Race Condition in Caching",
                "content": "Problem: Race condition detected in the caching layer when multiple threads update the same key simultaneously.",
                "node_type": "problem",
                "queries": ["race condition", "caching issue", "multiple threads update"]
            },
            {
                "name": "Concept",
                "label": "Idempotency in APIs",
                "content": "Concept: Idempotency means an operation produces the same result whether called once or multiple times. Critical for retry safety.",
                "node_type": "concept",
                "queries": ["idempotency", "retry safety", "same result multiple times"]
            }
        ]
        
        created_nodes = []
        
        # Create all test nodes
        print("\nCreating test nodes...")
        for tc in test_cases:
            status, data = api_post("/nodes/", {
                "label": tc["label"],
                "node_type": tc["node_type"],
                "content": tc["content"],
                "summary": f"Test: {tc['name']}",
                "namespace": "discriminating_test",
                "workspace_name": "hermes"
            })
            if status == 201:
                created_nodes.append({"id": data["id"], **tc})
                print(f"  ✅ {tc['name']}")
            else:
                print(f"  ❌ {tc['name']}: {data}")
        
        # Wait for embeddings
        print("\nWaiting 8s for embeddings...")
        time.sleep(8)
        
        # Test each query
        print("\nTesting recall...")
        matrix = []
        
        for node in created_nodes:
            node_results = {"name": node["name"], "queries": []}
            
            for query in node["queries"]:
                url = f"{API_V1}/search?q={urllib.parse.quote(query)}&limit=10&namespace=discriminating_test"
                try:
                    req = urllib.request.Request(url, method="GET")
                    with urllib.request.urlopen(req, timeout=10) as resp:
                        data = json.loads(resp.read().decode())
                        results = data if isinstance(data, list) else data.get("results", [])
                        
                        found = any(node["id"] == r.get("node_id") for r in results)
                        rank = next((i+1 for i, r in enumerate(results) if node["id"] == r.get("node_id")), None)
                        
                        node_results["queries"].append({
                            "query": query,
                            "found": found,
                            "rank": rank,
                            "total_results": len(results)
                        })
                        
                        status_icon = "✅" if found else "❌"
                        rank_str = f"(rank #{rank})" if rank else ""
                        print(f"  {status_icon} '{query}' -> {node['name'][:30]} {rank_str}")
                except Exception as e:
                    node_results["queries"].append({"query": query, "error": str(e), "found": False})
                    print(f"  ❌ '{query}' -> ERROR")
            
            matrix.append(node_results)
        
        # Calculate statistics
        total_queries = sum(len(n["queries"]) for n in matrix)
        found_queries = sum(1 for n in matrix for q in n["queries"] if q.get("found"))
        
        print(f"\n{'='*60}")
        print(f"RESULTS: {found_queries}/{total_queries} queries successful ({found_queries/max(total_queries,1)*100:.0f}%)")
        print(f"{'='*60}")
        
        # Per-node-type breakdown
        print("\nPer node type:")
        for node in matrix:
            queries = node["queries"]
            found = sum(1 for q in queries if q.get("found"))
            print(f"  {node['name']}: {found}/{len(queries)}")
        
        # Cleanup
        print("\nCleaning up test nodes...")
        for node in created_nodes:
            api_post(f"/nodes/{node['id']}", {"valid_to": datetime.now(timezone.utc).isoformat()})
        
        return matrix
    
    # -------------------------------------------------------------------------
    # SUMMARY AND RECOMMENDATIONS
    # -------------------------------------------------------------------------
    def generate_report(self):
        print("\n" + "="*60)
        print("AUDIT SUMMARY")
        print("="*60)
        
        critical = sum(1 for f in self.findings if f["severity"] == "CRITICAL")
        high = sum(1 for f in self.findings if f["severity"] == "HIGH")
        medium = sum(1 for f in self.findings if f["severity"] == "MEDIUM")
        low = sum(1 for f in self.findings if f["severity"] == "LOW")
        
        print(f"\nFindings:")
        print(f"  🔴 Critical: {critical}")
        print(f"  🟠 High:     {high}")
        print(f"  🟡 Medium:   {medium}")
        print(f"  🟢 Low:      {low}")
        
        if critical > 0 or high > 0:
            print(f"\n⚠️  CRITICAL/HIGH issues found - recall will be unreliable")
        elif medium > 0:
            print(f"\n⚠️  Medium issues found - recall can be improved")
        else:
            print(f"\n✅ No significant issues found")
        
        print(f"\nRecommendations:")
        
        # Check for specific issues and recommend
        has_embedding_issue = any(f["category"] == "EMBEDDING" and f["severity"] in ["CRITICAL", "HIGH"] for f in self.findings)
        has_search_issue = any(f["category"] == "SEARCH" and f["severity"] in ["CRITICAL", "HIGH"] for f in self.findings)
        has_content_issue = any(f["category"] == "CONTENT" for f in self.findings)
        has_namespace_issue = any(f["category"] == "NAMESPACE" for f in self.findings)
        has_epistemic_issue = any(f["category"] == "EPISTEMIC" for f in self.findings)
        
        if has_embedding_issue:
            print("  1. Fix embedding pipeline - process pending queue, retry failures")
        if has_search_issue:
            print("  2. Tune search ranking - adjust RRF weights, check vector similarity")
        if has_content_issue:
            print("  3. Improve content quality - filter short content, reduce truncation")
        if has_namespace_issue:
            print("  4. Fix namespace filtering - ensure cross-namespace search works")
        if has_epistemic_issue:
            print("  5. Enable auto-classification - apply epistemic labels on creation")
        
        if not any([has_embedding_issue, has_search_issue, has_content_issue, has_namespace_issue, has_epistemic_issue]):
            print("  - System is healthy. To improve recall further:")
            print("    * Increase embedding model quality (larger model)")
            print("    * Add synonym expansion to search")
            print("    * Implement query rewriting")
        
        self.cur.close()
        self.conn.close()


def main():
    print("="*60)
    print("MINDBANK DEEP RECALL AUDIT")
    print("Using Praxis Diagnostic Reasoning + Superpowers Debugging")
    print("="*60)
    
    auditor = RecallAuditor()
    
    # Run all hypothesis tests
    auditor.test_embedding_pipeline()
    auditor.test_search_ranking()
    auditor.test_content_quality()
    auditor.test_namespace_filtering()
    auditor.test_epistemic_labels()
    
    # Run discriminating test
    matrix = auditor.run_discriminating_test()
    
    # Generate report
    auditor.generate_report()
    
    # Save detailed results
    report = {
        "timestamp": datetime.now(timezone.utc).isoformat(),
        "findings": auditor.findings,
        "discriminating_matrix": matrix
    }
    
    with open("/home/rat/mindbank/recall_audit_report.json", "w") as f:
        json.dump(report, f, indent=2, default=str)
    
    print(f"\n📄 Full report saved to: /home/rat/mindbank/recall_audit_report.json")


if __name__ == "__main__":
    main()
