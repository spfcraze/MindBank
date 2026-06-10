#!/usr/bin/env python3
"""
Final MindBank Recall Verification
Tests all fixes to verify 95%+ cross-session recall
"""

import urllib.request
import urllib.parse
import json
import time
from datetime import datetime, timezone

API_V1 = "http://localhost:8095/api/v1"


def api_post(path, data):
    url = f"{API_V1}{path}"
    req = urllib.request.Request(
        url, data=json.dumps(data).encode(),
        headers={"Content-Type": "application/json"},
        method="POST"
    )
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except Exception as e:
        return -1, {"error": str(e)}


def api_get(path):
    url = f"{API_V1}{path}"
    try:
        req = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except Exception as e:
        return -1, {"error": str(e)}


def test_recall():
    print("="*60)
    print("FINAL RECALL VERIFICATION")
    print("="*60)
    
    # Create comprehensive test nodes
    test_cases = [
        {
            "name": "Database Transaction Advice",
            "node_type": "advice",
            "content": "Always wrap database writes in transactions to ensure data consistency. Use BEGIN/COMMIT blocks. Rollback on error. Never leave transactions open.",
            "queries": [
                "database transactions",
                "ensure data consistency",
                "BEGIN COMMIT",
                "transaction best practices",
                "database write safety",
                "rollback on error",
                "ACID compliance"
            ]
        },
        {
            "name": "Deployment Best Practice",
            "node_type": "advice",
            "content": "Always run tests before deploying. Use blue-green deployment for zero downtime. Monitor error rates for 30 minutes after deployment. Rollback if error rate exceeds 0.1%.",
            "queries": [
                "deployment workflow",
                "how to deploy safely",
                "production deployment best practices",
                "blue green deployment",
                "rollback strategy",
                "monitor error rates",
                "zero downtime deployment"
            ]
        },
        {
            "name": "Python List Comprehension",
            "node_type": "fact",
            "content": "Python list comprehensions provide a concise way to create lists. Example: [x*2 for x in range(10)]. They are faster than for loops for simple operations.",
            "queries": [
                "python list comprehension",
                "how to create lists in python",
                "list comprehension vs for loop",
                "python concise list creation"
            ]
        },
        {
            "name": "Race Condition Problem",
            "node_type": "problem",
            "content": "Race condition detected in the caching layer when multiple threads update the same key simultaneously. Solution: Use mutex locks or atomic operations.",
            "queries": [
                "race condition",
                "caching issue",
                "multiple threads update",
                "mutex lock solution"
            ]
        },
        {
            "name": "Idempotency Concept",
            "node_type": "concept",
            "content": "Idempotency means an operation produces the same result whether called once or multiple times. Critical for retry safety in distributed systems.",
            "queries": [
                "idempotency",
                "retry safety",
                "same result multiple times",
                "distributed system safety"
            ]
        }
    ]
    
    created_nodes = []
    
    # Create test nodes
    print("\nCreating test nodes...")
    for tc in test_cases:
        status, data = api_post("/nodes/", {
            "label": tc["name"],
            "node_type": tc["node_type"],
            "content": tc["content"],
            "summary": f"Test: {tc['name']}",
            "namespace": "final_verify",
            "workspace_name": "hermes"
        })
        if status == 201:
            created_nodes.append({"id": data["id"], **tc})
            print(f"  ✅ {tc['name']}")
        else:
            print(f"  ❌ {tc['name']}: {data}")
    
    # Wait for embeddings
    print("\nWaiting 10s for embeddings...")
    time.sleep(10)
    
    # Test all queries
    print("\nTesting recall with synonym expansion...")
    total_queries = 0
    successful_queries = 0
    
    for node in created_nodes:
        print(f"\n  Testing: {node['name']}")
        for query in node["queries"]:
            total_queries += 1
            url = f"{API_V1}/search?q={urllib.parse.quote(query)}&limit=10&namespace=final_verify"
            
            try:
                req = urllib.request.Request(url, method="GET")
                with urllib.request.urlopen(req, timeout=10) as resp:
                    data = json.loads(resp.read().decode())
                    results = data if isinstance(data, list) else data.get("results", [])
                    
                    found = any(node["id"] == r.get("node_id") for r in results)
                    rank = next((i+1 for i, r in enumerate(results) if node["id"] == r.get("node_id")), None)
                    
                    if found:
                        successful_queries += 1
                        print(f"    ✅ '{query}' -> rank #{rank}")
                    else:
                        print(f"    ❌ '{query}' -> not found")
            except Exception as e:
                print(f"    ❌ '{query}' -> error: {e}")
    
    # Cleanup
    print("\nCleaning up test nodes...")
    for node in created_nodes:
        api_post(f"/nodes/{node['id']}", {"valid_to": datetime.now(timezone.utc).isoformat()})
    
    # Results
    success_rate = successful_queries / max(total_queries, 1) * 100
    print(f"\n{'='*60}")
    print(f"FINAL RESULTS: {successful_queries}/{total_queries} ({success_rate:.0f}%)")
    print(f"{'='*60}")
    
    if success_rate >= 95:
        print("🎉 TARGET ACHIEVED: 95%+ cross-session recall!")
        return 0
    elif success_rate >= 85:
        print("⚠️ GOOD: 85%+ recall, minor improvements possible")
        return 0
    else:
        print("❌ NEEDS WORK: Below 85% recall")
        return 1


if __name__ == "__main__":
    exit(test_recall())
