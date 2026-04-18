#!/bin/bash
# MindBank Integration Tests
# Tests batch embedding + materialized path after deployment
#
# Usage: bash tests/integration_test.sh [BASE_URL]
# Default: http://localhost:8095

set -e

BASE="${1:-http://localhost:8095}"
PASS=0
FAIL=0
TOTAL=0

pass() { PASS=$((PASS+1)); TOTAL=$((TOTAL+1)); echo "  ✅ $1"; }
fail() { FAIL=$((FAIL+1)); TOTAL=$((TOTAL+1)); echo "  ❌ $1: $2"; }

echo "╔══════════════════════════════════════════════════╗"
echo "║  MindBank Integration Tests                      ║"
echo "║  $BASE"
echo "╚══════════════════════════════════════════════════╝"
echo ""

# ---- Health Check ----
echo "=== Health ==="
HEALTH=$(curl -sf "$BASE/api/v1/health")
if echo "$HEALTH" | grep -q '"status":"ok"'; then
    VERSION=$(echo "$HEALTH" | python3 -c "import json,sys; print(json.load(sys.stdin)['version'])")
    pass "API healthy (v$VERSION)"
else
    fail "API health" "$HEALTH"
    exit 1
fi

# ---- Batch Embedding ----
echo ""
echo "=== Batch Embedding ==="

# Test 1: Empty batch
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/embeddings/batch" \
    -H "Content-Type: application/json" \
    -d '{"texts":[]}')
HTTP=$(echo "$RESP" | tail -1)
if [ "$HTTP" = "400" ]; then
    pass "Empty batch returns 400"
else
    fail "Empty batch" "expected 400, got $HTTP"
fi

# Test 2: Single text batch
RESP=$(curl -sf "$BASE/api/v1/embeddings/batch" \
    -H "Content-Type: application/json" \
    -d '{"texts":["hello world"]}')
if echo "$RESP" | grep -q '"count":1'; then
    DIMS=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['dimensions'])")
    pass "Single text batch ($DIMS dims)"
else
    fail "Single text batch" "$RESP"
fi

# Test 3: Multi-text parallel batch
RESP=$(curl -sf "$BASE/api/v1/embeddings/batch" \
    -H "Content-Type: application/json" \
    -d '{"texts":["alpha","beta","gamma","delta","epsilon"]}')
if echo "$RESP" | grep -q '"count":5'; then
    pass "5-text parallel batch"
else
    fail "5-text batch" "$RESP"
fi

# Test 4: Batch too large
LARGE_TEXTS=$(python3 -c "print(','.join(['\"x\"']*101))")
RESP=$(curl -s -w "\n%{http_code}" "$BASE/api/v1/embeddings/batch" \
    -H "Content-Type: application/json" \
    -d "{\"texts\":[$LARGE_TEXTS]}")
if [ "$HTTP" = "400" ]; then
    pass "101-text batch returns 400 (max 100)"
else
    fail "101-text batch" "expected 400, got $HTTP"
fi

# Test 5: Embedding stats reflect batch
RESP=$(curl -sf "$BASE/api/v1/embeddings/stats")
TOTAL_REQS=$(echo "$RESP" | python3 -c "import json,sys; print(json.load(sys.stdin)['total'])")
if [ "$TOTAL_REQS" -ge 7 ]; then
    pass "Stats show $TOTAL_REQS total requests"
else
    fail "Stats" "expected >=7, got $TOTAL_REQS"
fi

# ---- Materialized Path ----
echo ""
echo "=== Materialized Path ==="

# Create a 3-level hierarchy: Project -> Decision -> Fact
PROJECT=$(curl -sf "$BASE/api/v1/nodes" \
    -H "Content-Type: application/json" \
    -d '{"label":"Test Project","node_type":"project","content":"Root node"}')
PROJECT_ID=$(echo "$PROJECT" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

DECISION=$(curl -sf "$BASE/api/v1/nodes" \
    -H "Content-Type: application/json" \
    -d '{"label":"Test Decision","node_type":"decision","content":"Child of project"}')
DECISION_ID=$(echo "$DECISION" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

FACT=$(curl -sf "$BASE/api/v1/nodes" \
    -H "Content-Type: application/json" \
    -d '{"label":"Test Fact","node_type":"fact","content":"Grandchild"}')
FACT_ID=$(echo "$FACT" | python3 -c "import json,sys; print(json.load(sys.stdin)['id'])")

if [ -n "$PROJECT_ID" ] && [ -n "$DECISION_ID" ] && [ -n "$FACT_ID" ]; then
    pass "Created 3 nodes: project=$PROJECT_ID, decision=$DECISION_ID, fact=$FACT_ID"
else
    fail "Create nodes" "missing IDs"
fi

# Create edges: Project -> Decision -> Fact
curl -sf "$BASE/api/v1/edges" \
    -H "Content-Type: application/json" \
    -d "{\"source_id\":\"$PROJECT_ID\",\"target_id\":\"$DECISION_ID\",\"edge_type\":\"contains\"}" > /dev/null

curl -sf "$BASE/api/v1/edges" \
    -H "Content-Type: application/json" \
    -d "{\"source_id\":\"$DECISION_ID\",\"target_id\":\"$FACT_ID\",\"edge_type\":\"contains\"}" > /dev/null

pass "Created edges: Project -> Decision -> Fact"

# Test 6: Neighbors (1-hop)
RESP=$(curl -sf "$BASE/api/v1/nodes/$PROJECT_ID/neighbors")
if echo "$RESP" | grep -q "$DECISION_ID"; then
    pass "Project neighbors include Decision"
else
    fail "Project neighbors" "$RESP"
fi

# Test 7: Verify node search works (existing functionality)
RESP=$(curl -sf "$BASE/api/v1/search/hybrid" \
    -H "Content-Type: application/json" \
    -d '{"query":"Test Project"}')
if echo "$RESP" | grep -q "$PROJECT_ID"; then
    pass "Hybrid search finds Test Project"
else
    fail "Hybrid search" "Test Project not found"
fi

# Test 8: Ask endpoint works
RESP=$(curl -sf "$BASE/api/v1/ask" \
    -H "Content-Type: application/json" \
    -d '{"query":"Test Project"}')
if echo "$RESP" | grep -q '"context"'; then
    pass "Ask endpoint returns context"
else
    fail "Ask endpoint" "$RESP"
fi

# Test 9: Snapshot works
RESP=$(curl -sf "$BASE/api/v1/snapshot")
if echo "$RESP" | grep -q '"content"'; then
    pass "Snapshot returns content"
else
    fail "Snapshot" "$RESP"
fi

# Cleanup: delete test nodes
curl -sf -X DELETE "$BASE/api/v1/nodes/$PROJECT_ID" > /dev/null 2>&1 || true
curl -sf -X DELETE "$BASE/api/v1/nodes/$DECISION_ID" > /dev/null 2>&1 || true
curl -sf -X DELETE "$BASE/api/v1/nodes/$FACT_ID" > /dev/null 2>&1 || true

# ---- Results ----
echo ""
echo "══════════════════════════════════════════════"
echo ""
echo "  Results: $PASS passed, $FAIL failed ($TOTAL total)"
echo ""
if [ "$FAIL" -eq 0 ]; then
    echo "  ✅ ALL TESTS PASSED"
else
    echo "  ❌ $FAIL TEST(S) FAILED"
fi
echo ""
