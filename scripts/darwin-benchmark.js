// Darwin Benchmark: Observer Perspective
// Validates latency and correctness of /analyze/dependence, /synchronize, /observability

const BASE = "http://127.0.0.1:8095/api/v1";
const SEED_ID = "297fb268-4c58-49cd-bcd5-bd2a8c5e1c6c"; // high-degree node

async function timeIt(name, fn) {
  const start = Date.now();
  const result = await fn();
  const ms = Date.now() - start;
  return { name, ms, result };
}

async function post(path, body) {
  const res = await fetch(BASE + path, {
    method: "POST",
    headers: { "Content-Type": "application/json" },
    body: JSON.stringify(body),
  });
  if (!res.ok) throw new Error(`${path} ${res.status}`);
  return res.json();
}

async function get(path) {
  const res = await fetch(BASE + path);
  if (!res.ok) throw new Error(`${path} ${res.status}`);
  return res.json();
}

async function main() {
  console.log("=== Darwin Benchmark: Observer Perspective ===\n");

  // 1. Latency: /analyze/dependence
  const dep = await timeIt("/analyze/dependence", () =>
    post("/analyze/dependence", { node_id: SEED_ID, max_depth: 3 })
  );
  console.log(`${dep.name}: ${dep.ms}ms (target: <50ms) ${dep.ms < 50 ? "PASS" : "WARN"}`);

  // Validate dependence structure
  const depData = dep.result;
  const hasNodes = depData.dependence_graph?.nodes?.length > 0;
  const hasEdges = depData.dependence_graph?.edges?.length > 0;
  const hasModes = depData.influence_modes?.length > 0;
  const criticalDepth = depData.critical_depth;
  console.log(`  nodes=${depData.dependence_graph?.nodes?.length}, edges=${depData.dependence_graph?.edges?.length}, critical_depth=${criticalDepth}`);
  console.log(`  hasNodes=${hasNodes}, hasEdges=${hasEdges}, hasModes=${hasModes}`);

  // 2. Latency: /analyze/synchronize
  const sync = await timeIt("/analyze/synchronize", () =>
    post("/analyze/synchronize", { node_id: SEED_ID, propagate_depth: 2 })
  );
  console.log(`\n${sync.name}: ${sync.ms}ms (target: <100ms) ${sync.ms < 100 ? "PASS" : "WARN"}`);
  const syncData = sync.result;
  console.log(`  affected_nodes=${syncData.affected_nodes}, confidence_updates=${syncData.confidence_updates?.length}`);

  // 3. Latency: /analyze/observability
  const obs = await timeIt("/analyze/observability", () =>
    get(`/analyze/observability?namespace=klixsor&seed_node_ids=${SEED_ID}`)
  );
  console.log(`\n${obs.name}: ${obs.ms}ms (target: <50ms) ${obs.ms < 50 ? "PASS" : "WARN"}`);
  const obsData = obs.result;
  console.log(`  observable=${obsData.observable_nodes}/${obsData.total_nodes}, ratio=${(obsData.observability_ratio * 100).toFixed(1)}%`);

  // 4. Correctness: critical depth should be <= max_depth
  const depthOk = criticalDepth <= 3;
  console.log(`\nCorrectness checks:`);
  console.log(`  critical_depth <= max_depth: ${depthOk ? "PASS" : "FAIL"}`);
  console.log(`  coverage in [0,1]: ${depData.coverage >= 0 && depData.coverage <= 1 ? "PASS" : "FAIL"}`);
  console.log(`  observability_ratio in [0,1]: ${obsData.observability_ratio >= 0 && obsData.observability_ratio <= 1 ? "PASS" : "FAIL"}`);

  // 5. Summary score
  const score =
    (dep.ms < 50 ? 25 : dep.ms < 100 ? 15 : 5) +
    (sync.ms < 100 ? 25 : sync.ms < 200 ? 15 : 5) +
    (obs.ms < 50 ? 25 : obs.ms < 100 ? 15 : 5) +
    (depthOk && depData.coverage >= 0 && depData.coverage <= 1 ? 25 : 0);

  console.log(`\n=== Darwin Score: ${score}/100 ===`);
  if (score >= 80) {
    console.log("✓ PASS: Observer perspective implementation is solid");
    process.exit(0);
  } else if (score >= 60) {
    console.log("⚠ ACCEPTABLE: Some latency or correctness issues detected");
    process.exit(0);
  } else {
    console.log("✗ FAIL: Needs improvement before shipping");
    process.exit(1);
  }
}

main().catch((e) => {
  console.error("Benchmark failed:", e.message);
  process.exit(1);
});
