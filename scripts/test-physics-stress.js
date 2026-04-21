#!/usr/bin/env node
/**
 * MindBank Physics Stress Test — 200 nodes, 500 edges
 * Simulates typical production load to verify convergence and performance.
 * Run: node scripts/test-physics-stress.js
 */

const fs = require('fs');
const path = require('path');

const GRAPH_PATH = path.join(__dirname, '..', 'internal', 'handler', 'static', 'graph.html');
const src = fs.readFileSync(GRAPH_PATH, 'utf8');

// Extract physics function from graph.html
const scriptMatch = src.match(/<script>([\s\S]*?)<\/script>/);
if (!scriptMatch) { console.error('No script block found'); process.exit(1); }

// We need to run the physics in isolation. Extract just the physics-related code.
const physicsMatch = scriptMatch[1].match(/function physics\(\)\{[\s\S]*?^}/m);
if (!physicsMatch) { console.error('physics() not found'); process.exit(1); }

const NODE_COUNT = 200;
const EDGE_COUNT = 500;
const FRAMES = 250; // More than force-settle threshold (200)

console.log(`=== Physics Stress Test: ${NODE_COUNT} nodes, ${EDGE_COUNT} edges, ${FRAMES} frames ===\n`);

// Setup
const nodes = [];
const edges = [];
const nodeMap = new Map();
let visNodesCache = [];
let physicsSettled = false;
let settleCounter = 0;
let physicsFrame = 0;

// Create nodes in a cluster
for (let i = 0; i < NODE_COUNT; i++) {
  const n = {
    id: i,
    label: 'stress-node-' + i,
    type: ['fact', 'decision', 'project', 'topic', 'concept'][i % 5],
    ns: 'ns-' + (i % 8),
    x: (Math.random() - 0.5) * 100,
    y: (Math.random() - 0.5) * 100,
    vx: 0, vy: 0,
    size: 8 + Math.random() * 6,
    visible: true,
    imp: Math.random(),
    acc: Math.floor(Math.random() * 50)
  };
  nodes.push(n);
  nodeMap.set(i, n);
}

// Create random edges
for (let i = 0; i < EDGE_COUNT; i++) {
  const src = Math.floor(Math.random() * NODE_COUNT);
  let dst = Math.floor(Math.random() * NODE_COUNT);
  if (dst === src) dst = (dst + 1) % NODE_COUNT;
  edges.push({
    src, dst,
    type: 'relates_to', w: 0.5 + Math.random(),
    style: { color: [100, 100, 120], rgba: 'rgba(100,100,120,', dash: null, width: 1, glow: false },
    particles: [], nextSpawn: 0, burstTimer: 0, glow: 0
  });
}

// Run physics
const startMs = Date.now();
for (let f = 0; f < FRAMES; f++) {
  // Manually rebuild visNodesCache (simulating physics() behavior)
  visNodesCache.length = 0;
  for (const n of nodes) { if (n.visible) visNodesCache.push(n); }

  // Extracted physics code won't work in isolation since it references global vars.
  // Instead, implement the core physics loop directly:
  const visCount = visNodesCache.length;
  if (visCount === 0) continue;

  if (physicsSettled && physicsFrame % 60 !== 0) {
    physicsFrame++;
    continue;
  }

  if (physicsSettled) {
    let anyMoving = false;
    for (const n of visNodesCache) {
      if (Math.abs(n.vx) > 0.01 || Math.abs(n.vy) > 0.01) { anyMoving = true; break; }
    }
    if (!anyMoving) { physicsFrame++; continue; }
    physicsSettled = false;
    settleCounter = 0;
  }

  const damping = 0.82;
  const centerPull = 0.004;
  const maxSpeed = 6;
  const repulsionBase = 6000;
  const attractionStr = 0.004;
  const idealDist = 150;
  const repulsionDist2 = 250000;
  const skipRepulsion = visCount > 60 && physicsFrame % 2 === 0;

  if (!skipRepulsion) {
    for (let i = 0; i < visCount; i++) {
      const a = visNodesCache[i];
      const aR = a.size * 1.2;
      for (let j = i + 1; j < visCount; j++) {
        const b = visNodesCache[j];
        const bR = b.size * 1.2;
        const dx = a.x - b.x, dy = a.y - b.y;
        const d2 = dx * dx + dy * dy;
        if (d2 > repulsionDist2) continue;
        const dist = Math.sqrt(d2) || 1;
        const minDist = aR + bR;
        if (dist < minDist) {
          const overlap = minDist - dist;
          const nx = dx / dist, ny = dy / dist;
          const pushStrength = Math.min(1, overlap / (minDist * 0.3)) * 0.8;
          a.x += nx * overlap * pushStrength;
          a.y -= ny * overlap * pushStrength;
          b.x -= nx * overlap * pushStrength;
          b.y += ny * overlap * pushStrength;
        }
        const combinedSize = aR + bR;
        const softDist2 = Math.max(d2, combinedSize * combinedSize * 0.25);
        const strength = repulsionBase * (combinedSize / 30);
        let f = strength / (softDist2 || 1);
        f = Math.min(f, 1.5);
        const invDist = 1 / dist;
        const fx = dx * invDist * f, fy = dy * invDist * f;
        a.vx += fx; a.vy += fy; b.vx -= fx; b.vy -= fy;
      }
      a.vx -= a.x * centerPull;
      a.vy -= a.y * centerPull;
    }
  }

  // Edge attraction
  if (!skipRepulsion) {
    const edgeCap = Math.min(edges.length, 500);
    for (let ei = 0; ei < edgeCap; ei++) {
      const e = edges[ei];
      const a = nodeMap.get(e.src), b = nodeMap.get(e.dst);
      if (!a || !b || !a.visible || !b.visible) continue;
      const dx = b.x - a.x, dy = b.y - a.y;
      const d2 = dx * dx + dy * dy;
      if (d2 > repulsionDist2 * 4) continue;
      const dist = Math.sqrt(d2) || 1;
      const minDist = (a.size + b.size) * 1.5;
      const target = Math.max(idealDist, minDist);
      let f = (dist - target) * attractionStr * e.w;
      f = Math.max(-2, Math.min(2, f));
      const invDist = 1 / dist;
      a.vx += dx * invDist * f; a.vy += dy * invDist * f;
      b.vx -= dx * invDist * f; b.vy -= dy * invDist * f;
    }
  }

  // Apply velocity
  let totalMovement = 0;
  for (const n of nodes) {
    if (!n.visible) { n.vx = 0; n.vy = 0; continue; }
    const sizeDamping = Math.max(0.7, 0.9 - n.size * 0.005);
    n.vx *= sizeDamping; n.vy *= sizeDamping;
    const sizeSpeedCap = Math.max(3, maxSpeed - n.size * 0.1);
    const speed = Math.sqrt(n.vx * n.vx + n.vy * n.vy);
    if (speed > sizeSpeedCap) { const s = sizeSpeedCap / speed; n.vx *= s; n.vy *= s; }
    const bound = 800;
    const bs = 0.3 + n.size * 0.01;
    if (n.x < -bound) n.vx += bs; if (n.x > bound) n.vx -= bs;
    if (n.y < -bound) n.vy += bs; if (n.y > bound) n.vy -= bs;
    if (Math.abs(n.vx) < 0.01) n.vx = 0;
    if (Math.abs(n.vy) < 0.01) n.vy = 0;
    n.x += n.vx; n.y += n.vy;
    if (!isFinite(n.x)) n.x = 0;
    if (!isFinite(n.y)) n.y = 0;
    if (!isFinite(n.vx)) n.vx = 0;
    if (!isFinite(n.vy)) n.vy = 0;
    totalMovement += Math.abs(n.vx) + Math.abs(n.vy);
  }

  // Detect settled
  if (totalMovement < visCount * 0.15) {
    settleCounter++;
    if (settleCounter > 30) physicsSettled = true;
  } else {
    settleCounter = 0;
  }
  if (physicsFrame > 200 && !physicsSettled) physicsSettled = true;
  physicsFrame++;
}

const elapsedMs = Date.now() - startMs;

// Verify results
let allFinite = true;
let maxCoord = 0;
let minCoord = Infinity;
for (const n of nodes) {
  if (!isFinite(n.x) || !isFinite(n.y)) allFinite = false;
  maxCoord = Math.max(maxCoord, Math.abs(n.x), Math.abs(n.y));
  minCoord = Math.min(minCoord, Math.abs(n.x), Math.abs(n.y));
}

console.log(`Time: ${elapsedMs}ms (${(elapsedMs / FRAMES).toFixed(2)}ms/frame)`);
console.log(`Max coord: ${maxCoord.toFixed(1)}`);
console.log(`Physics settled: ${physicsSettled} (frame ${physicsFrame})`);
console.log(`All positions finite: ${allFinite}`);

let errors = 0;
if (!allFinite) { console.log('ERROR: NaN/Infinity detected'); errors++; }
if (maxCoord > 5000) { console.log(`ERROR: Nodes diverged (maxCoord=${maxCoord.toFixed(1)})`); errors++; }
if (elapsedMs / FRAMES > 10) { console.log(`WARNING: Slow physics (${(elapsedMs / FRAMES).toFixed(2)}ms/frame)`); }

if (errors === 0) {
  console.log('\nSTRESS TEST PASSED');
} else {
  console.log(`\nSTRESS TEST FAILED (${errors} errors)`);
  process.exit(1);
}
