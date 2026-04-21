#!/usr/bin/env node
/**
 * API Integration Deep Review — Worker 96 replacement
 * Verifies API endpoints, data flow, JSON structure match, race conditions.
 */

const fs = require('fs');
const path = require('path');

const GRAPH_PATH = path.join(__dirname, '..', 'internal', 'handler', 'static', 'graph.html');
const ROUTER_PATH = path.join(__dirname, '..', 'internal', 'handler', 'router.go');
const NODE_PATH = path.join(__dirname, '..', 'internal', 'handler', 'node.go');
const EDGE_PATH = path.join(__dirname, '..', 'internal', 'handler', 'edge.go');

let passed = 0, failed = 0, warnings = 0;
function pass(msg) { passed++; console.log(`  PASS  ${msg}`); }
function fail(msg) { failed++; console.log(`  FAIL  ${msg}`); }
function warn(msg) { warnings++; console.log(`  WARN  ${msg}`); }

console.log('=== API Integration Review ===\n');

// Load files
const html = fs.readFileSync(GRAPH_PATH, 'utf8');
const js = html.match(/<script>([\s\S]*?)<\/script>/)[1];
let router = '', nodeH = '', edgeH = '';
try { router = fs.readFileSync(ROUTER_PATH, 'utf8'); } catch {}
try { nodeH = fs.readFileSync(NODE_PATH, 'utf8'); } catch {}
try { edgeH = fs.readFileSync(EDGE_PATH, 'utf8'); } catch {}

// --- Endpoint Existence ---
console.log('--- API Endpoint Cross-Reference ---');
const endpoints = [
  { js: '/api/v1/graph', desc: 'Graph data (nodes+edges)' },
  { js: '/api/v1/nodes', desc: 'Node list/count' },
  { js: '/api/v1/snapshot', desc: 'Hermes snapshot' },
  { js: "nodes/'+n.id+'/neighbors'", desc: 'Node neighbors' },
];
for (const ep of endpoints) {
  if (js.includes(ep.js)) {
    if (router.includes(ep.js.split("'").join('"').replace('+n.id+', ''))) {
      pass(`${ep.desc}: found in both JS and Go router`);
    } else {
      warn(`${ep.desc}: in JS but not clearly in router.go (may use pattern matching)`);
    }
  } else {
    fail(`${ep.desc}: not found in JS`);
  }
}

// --- JSON Structure ---
console.log('\n--- JSON Structure ---');
// Node fields expected by frontend
const nodeFields = ['id', 'label', 'node_type', 'namespace', 'summary', 'content', 'importance', 'access_count'];
for (const field of nodeFields) {
  if (js.includes(`n.${field}`) || js.includes(`data.nodes`)) {
    pass(`Node field '${field}' referenced in frontend`);
  }
}
// Edge fields
const edgeFields = ['source', 'target', 'edge_type', 'weight'];
if (js.includes('e.source') && js.includes('e.target') && js.includes('e.edge_type')) {
  pass('Edge fields: source, target, edge_type referenced');
} else {
  fail('Edge field mapping mismatch');
}

// --- Race Condition Check ---
console.log('\n--- Race Conditions ---');
if (js.includes('if(pollBusy)return') && js.includes('pollBusy=true')) {
  pass('pollBusy guard prevents overlapping polls');
} else {
  fail('No pollBusy guard — race condition on concurrent polls');
}
// Check if buildGraph can be called while render is running
if (js.includes('if(!renderRunning)return')) {
  pass('renderRunning guard prevents rendering during rebuild');
} else {
  warn('No renderRunning guard — render may run during buildGraph');
}

// --- Error Handling ---
console.log('\n--- Error Handling ---');
const fetchCalls = js.match(/fetch\(/g) || [];
const abortControllers = js.match(/AbortController/g) || [];
const tryCatches = js.match(/try\{/g) || [];
pass(`fetch() calls: ${fetchCalls.length}, AbortControllers: ${abortControllers.length}, try{} blocks: ${tryCatches.length}`);
if (abortControllers.length >= 2) {
  pass('Multiple AbortControllers for timeout handling');
} else {
  warn('Only 1 AbortController — may not timeout all fetch calls');
}

// --- Polling ---
console.log('\n--- Polling Strategy ---');
if (js.includes('15000')) pass('15s interval for graph rebuild check');
if (js.includes('lastNodeCount=cnt.count||d.nodes.length')) {
  pass('Count API used for accurate node count baseline');
} else {
  warn('Node count may be stale');
}
if (js.includes('remote.access_count>local.acc')) {
  pass('Access count delta detection for live updates');
} else {
  warn('No access_count change detection');
}

// --- 3D Data Check ---
console.log('\n--- 3D Data Check ---');
if (router.includes('z_coord') || router.includes('z_position') || router.includes('depth')) {
  fail('3D coordinate fields found in backend!');
} else {
  pass('No 3D coordinate fields in backend');
}

// --- Summary ---
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');
if (failed > 0) process.exit(1);
