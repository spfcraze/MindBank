#!/usr/bin/env python3
"""
MindBank Hermes Integration Test
Simulates: create memory in Session A -> recall in Session B via MCP
Uses Praxis methodology: test -> verify -> report
"""

import subprocess
import json
import time
import uuid
import urllib.request
import urllib.error
import urllib.parse
from datetime import datetime

API_BASE = "http://localhost:8095"
MCP_BASE = f"{API_BASE}/mcp"
API_V1 = f"{API_BASE}/api/v1"


def api_call(method, path, data=None):
    """Make API call and return parsed JSON."""
    url = f"{API_V1}{path}"
    headers = {"Content-Type": "application/json"}
    
    if data:
        data = json.dumps(data).encode()
    
    req = urllib.request.Request(url, data=data, headers=headers, method=method)
    
    try:
        with urllib.request.urlopen(req, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except urllib.error.HTTPError as e:
        return e.code, json.loads(e.read().decode()) if e.read() else {"error": str(e)}
    except Exception as e:
        return -1, {"error": str(e)}


def mcp_call(endpoint):
    """Call MCP endpoint."""
    url = f"{MCP_BASE}/{endpoint}"
    try:
        with urllib.request.urlopen(url, timeout=10) as resp:
            return resp.status, json.loads(resp.read().decode())
    except Exception as e:
        return -1, {"error": str(e)}


def test_mcp_health():
    """Test 1: MCP health endpoint."""
    print("\n[TEST 1] MCP Health Check")
    print("-" * 50)
    
    status, data = mcp_call("health")
    
    if status == 200 and data.get("status") == "ok":
        print(f"✅ PASS: MCP health OK")
        print(f"   Service: {data.get('service')}")
        print(f"   Version: {data.get('version')}")
        return True
    else:
        print(f"❌ FAIL: Status {status}, Response: {data}")
        return False


def test_mcp_tools():
    """Test 2: MCP tools discovery."""
    print("\n[TEST 2] MCP Tools Discovery")
    print("-" * 50)
    
    status, data = mcp_call("tools")
    
    if status == 200 and "tools" in data:
        tools = data["tools"]
        print(f"✅ PASS: Found {len(tools)} MCP tools")
        for tool in tools:
            print(f"   - {tool['name']}: {tool['description'][:60]}...")
        return True
    else:
        print(f"❌ FAIL: Status {status}, Response: {data}")
        return False


def test_create_memory():
    """Test 3: Create a memory node (simulating Session A)."""
    print("\n[TEST 3] Create Memory Node (Session A)")
    print("-" * 50)
    
    test_content = {
        "label": f"Hermes Test Memory {datetime.now().isoformat()}",
        "node_type": "fact",
        "content": "This is a test memory created from a Hermes session to verify MindBank recall functionality across sessions.",
        "summary": "Cross-session memory test",
        "namespace": "hermes_test",
        "workspace_name": "hermes"
    }
    
    status, data = api_call("POST", "/nodes/", test_content)
    
    if status == 201 and "id" in data:
        node_id = data["id"]
        print(f"✅ PASS: Created memory node")
        print(f"   ID: {node_id}")
        print(f"   Label: {data.get('label')}")
        print(f"   Type: {data.get('node_type')}")
        return node_id
    else:
        print(f"❌ FAIL: Status {status}, Response: {data}")
        return None


def test_recall_by_search(query, expected_namespace="hermes_test"):
    """Test 4: Search for memory (simulating Session B recall)."""
    print(f"\n[TEST 4] Recall Memory via Search (Session B)")
    print(f"Query: '{query}'")
    print("-" * 50)
    
    # Use GET search endpoint with query params
    url = f"{API_V1}/search?q={urllib.parse.quote(query)}&limit=5"
    if expected_namespace:
        url += f"&namespace={expected_namespace}"
    
    try:
        req = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode())
            
            if isinstance(data, list):
                results = data
            else:
                results = data.get("results", [])
            
            print(f"✅ PASS: Search returned {len(results)} results")
            
            found_test = False
            for r in results:
                label = r.get("label", "N/A")
                score = r.get("rrf_score", r.get("fts_score", 0))
                print(f"   - {label[:60]}... (score: {score:.3f})")
                if "Hermes Test Memory" in label:
                    found_test = True
            
            if found_test:
                print("✅ Test memory FOUND in search results!")
                return True
            else:
                print("⚠️  Test memory not in top results (may need embedding time)")
                return False
    except Exception as e:
        print(f"❌ FAIL: {e}")
        return False


def test_direct_recall(node_id):
    """Test 5: Direct node retrieval by ID."""
    print(f"\n[TEST 5] Direct Recall by ID")
    print(f"Node ID: {node_id}")
    print("-" * 50)
    
    status, data = api_call("GET", f"/nodes/{node_id}")
    
    if status == 200 and "id" in data:
        print(f"✅ PASS: Retrieved node directly")
        print(f"   Label: {data.get('label')}")
        print(f"   Content: {data.get('content', '')[:100]}...")
        print(f"   Namespace: {data.get('namespace')}")
        return True
    else:
        print(f"❌ FAIL: Status {status}, Response: {data}")
        return False


def test_similarity_search(content):
    """Test 6: Semantic similarity search."""
    print(f"\n[TEST 6] Semantic Similarity Search")
    print(f"Content: '{content[:50]}...'")
    print("-" * 50)
    
    url = f"{API_V1}/search?q={urllib.parse.quote(content)}&limit=5"
    
    try:
        req = urllib.request.Request(url, method="GET")
        with urllib.request.urlopen(req, timeout=10) as resp:
            data = json.loads(resp.read().decode())
            
            if isinstance(data, list):
                results = data
            else:
                results = data.get("results", [])
            
            print(f"✅ PASS: Similarity search returned {len(results)} results")
            for r in results[:3]:
                label = r.get("label", "N/A")
                score = r.get("rrf_score", r.get("fts_score", 0))
                print(f"   - {label[:60]}... (score: {score:.3f})")
            return True
    except Exception as e:
        print(f"❌ FAIL: {e}")
        return False


def test_graph_neighbors(node_id):
    """Test 7: Graph neighbor exploration."""
    print(f"\n[TEST 7] Graph Neighbor Exploration")
    print(f"Node ID: {node_id}")
    print("-" * 50)
    
    status, data = api_call("GET", f"/nodes/{node_id}/neighbors")
    
    if status == 200:
        if isinstance(data, list):
            neighbors = data
        else:
            neighbors = data.get("neighbors", [])
        print(f"✅ PASS: Found {len(neighbors)} neighbors")
        for n in neighbors[:3]:
            if isinstance(n, dict):
                print(f"   - {n.get('label', 'N/A')[:60]}... (relation: {n.get('relation', 'N/A')})")
        return True
    else:
        print(f"❌ FAIL: Status {status}, Response: {data}")
        return False


def test_mcp_tool_execution():
    """Test 8: MCP tool schema validation."""
    print(f"\n[TEST 8] MCP Tool Schema Validation")
    print("-" * 50)
    
    status, data = mcp_call("tools")
    
    if status != 200:
        print(f"❌ FAIL: Could not fetch tools")
        return False
    
    tools = data.get("tools", [])
    valid_tools = 0
    
    for tool in tools:
        name = tool.get("name", "")
        schema = tool.get("inputSchema", {})
        
        if schema.get("type") == "object" and "properties" in schema:
            print(f"✅ {name}: Valid schema with {len(schema['properties'])} properties")
            valid_tools += 1
        else:
            print(f"❌ {name}: Invalid schema")
    
    print(f"\nValid tools: {valid_tools}/{len(tools)}")
    return valid_tools == len(tools)


def test_cross_session_recall():
    """Main test: Simulate cross-session memory recall."""
    print("\n" + "=" * 60)
    print("HERMES CROSS-SESSION MEMORY RECALL TEST")
    print("=" * 60)
    print("\nScenario:")
    print("  Session A: Create memory about 'deployment workflow'")
    print("  Session B: Search for 'how do I deploy'")
    print("  Expected: Find memory from Session A")
    
    # Session A: Create memory
    print("\n" + "=" * 60)
    print("SESSION A: Creating Memory")
    print("=" * 60)
    
    memory_content = {
        "label": "Deployment Workflow Best Practice",
        "node_type": "advice",
        "content": "Always run tests before deploying. Use blue-green deployment for zero downtime. Monitor error rates for 30 minutes after deployment. Rollback if error rate exceeds 0.1%.",
        "summary": "Deployment best practices for production",
        "namespace": "hermes_test",
        "workspace_name": "hermes"
    }
    
    status, data = api_call("POST", "/nodes/", memory_content)
    
    if status != 201 or "id" not in data:
        print(f"❌ FAIL: Could not create memory: {data}")
        return False
    
    node_id = data["id"]
    print(f"✅ Created memory: {node_id}")
    
    # Wait for embedding generation
    print("\nWaiting for embedding generation (5s)...")
    time.sleep(5)
    
    # Session B: Recall memory
    print("\n" + "=" * 60)
    print("SESSION B: Recalling Memory")
    print("=" * 60)
    
    # Test 1: Search with different wording
    queries = [
        "deployment workflow",
        "how to deploy safely",
        "production deployment best practices",
        "blue green deployment",
        "rollback strategy"
    ]
    
    found_count = 0
    for query in queries:
        url = f"{API_V1}/search?q={urllib.parse.quote(query)}&limit=10&namespace=hermes_test"
        
        try:
            req = urllib.request.Request(url, method="GET")
            with urllib.request.urlopen(req, timeout=10) as resp:
                data = json.loads(resp.read().decode())
                
                if isinstance(data, list):
                    results = data
                else:
                    results = data.get("results", [])
                
                found = any(node_id == r.get("node_id") for r in results)
                
                if found:
                    print(f"✅ Query '{query}' -> FOUND")
                    found_count += 1
                else:
                    print(f"⚠️  Query '{query}' -> not in top results")
        except Exception as e:
            print(f"❌ Query '{query}' -> error: {e}")
    
    print(f"\nRecall success: {found_count}/{len(queries)} queries found the memory")
    return found_count >= 2  # At least 2 queries should find it


def run_all_tests():
    """Run complete test suite."""
    print("\n" + "=" * 60)
    print("MINDBANK HERMES INTEGRATION TEST SUITE")
    print("=" * 60)
    print(f"API Base: {API_BASE}")
    print(f"Time: {datetime.now().isoformat()}")
    
    results = []
    
    # Basic connectivity
    results.append(("MCP Health", test_mcp_health()))
    results.append(("MCP Tools", test_mcp_tools()))
    results.append(("MCP Schema", test_mcp_tool_execution()))
    
    # Memory operations
    node_id = test_create_memory()
    if node_id:
        results.append(("Create Memory", True))
        
        # Wait for embedding
        print("\nWaiting for embedding generation (3s)...")
        time.sleep(3)
        
        results.append(("Direct Recall", test_direct_recall(node_id)))
        results.append(("Search Recall", test_recall_by_search("Hermes Test Memory")))
        results.append(("Similarity Search", test_similarity_search("cross-session memory test")))
        results.append(("Graph Neighbors", test_graph_neighbors(node_id)))
    else:
        results.append(("Create Memory", False))
        results.append(("Direct Recall", False))
        results.append(("Search Recall", False))
        results.append(("Similarity Search", False))
        results.append(("Graph Neighbors", False))
    
    # Cross-session test
    results.append(("Cross-Session Recall", test_cross_session_recall()))
    
    # Summary
    print("\n" + "=" * 60)
    print("TEST SUMMARY")
    print("=" * 60)
    
    passed = sum(1 for _, r in results if r)
    total = len(results)
    
    for name, result in results:
        status = "✅ PASS" if result else "❌ FAIL"
        print(f"{status}: {name}")
    
    print(f"\nTotal: {passed}/{total} tests passed ({passed/total*100:.0f}%)")
    
    if passed == total:
        print("\n🎉 ALL TESTS PASSED - MindBank is fully operational!")
    elif passed >= total * 0.7:
        print("\n⚠️  MOST TESTS PASSED - Minor issues detected")
    else:
        print("\n❌ CRITICAL ISSUES - MindBank needs attention")
    
    return passed, total


if __name__ == "__main__":
    passed, total = run_all_tests()
    exit(0 if passed == total else 1)
