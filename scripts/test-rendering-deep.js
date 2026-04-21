#!/usr/bin/env node
/**
 * Rendering Pipeline Deep Review — Worker 95 replacement
 * Tests LOD, viewport culling, shape rendering, no-3D-consistency.
 */

const fs = require('fs');
const path = require('path');

const GRAPH_PATH = path.join(__dirname, '..', 'internal', 'handler', 'static', 'graph.html');
let passed = 0, failed = 0, warnings = 0;

function pass(msg) { passed++; console.log(`  PASS  ${msg}`); }
function fail(msg) { failed++; console.log(`  FAIL  ${msg}`); }
function warn(msg) { warnings++; console.log(`  WARN  ${msg}`); }

const src = fs.readFileSync(GRAPH_PATH, 'utf8');
const js = src.match(/<script>([\s\S]*?)<\/script>/)[1];

console.log('=== Rendering Pipeline Deep Review ===\n');

// --- LOD System ---
console.log('--- LOD System ---');
const lodCheck = src.match(/const lod=cam\.z>(\d+\.?\d*)\?'high':cam\.z>(\d+\.?\d*)\?'medium':'low'/);
if (lodCheck) {
  pass(`LOD thresholds: high>${lodCheck[1]}, medium>${lodCheck[2]}, else low`);
} else {
  fail('LOD threshold logic not found');
}

// --- Edge Cap by LOD ---
console.log('\n--- Edge Rendering ---');
const edgeCap = src.match(/lod==='high'\?(\d+):lod==='medium'\?(\d+):(\d+)/);
if (edgeCap) {
  pass(`Edge caps: high=${edgeCap[1]}, medium=${edgeCap[2]}, low=${edgeCap[3]}`);
} else {
  fail('Edge cap by LOD not found');
}

// --- No gradients in drawNodes (performance) ---
console.log('\n--- Performance ---');
const drawIdx = src.indexOf('function drawNodes');
const drawBlock = src.substring(drawIdx, drawIdx + 3000);
if (drawBlock.includes('createRadialGradient') || drawBlock.includes('createLinearGradient')) {
  fail('GRADIENT IN drawNodes — FPS killer!');
} else {
  pass('No gradients per frame in drawNodes');
}

// --- globalAlpha reset ---
console.log('\n--- Alpha Reset ---');
const alphaResets = (src.match(/ctx\.globalAlpha=1/g) || []).length;
if (alphaResets >= 3) {
  pass(`globalAlpha=1 found ${alphaResets} times (properly reset after draw blocks)`);
} else {
  warn(`Only ${alphaResets} globalAlpha resets — may have alpha bleed`);
}

// --- Pre-computed rgba prefix ---
console.log('\n--- Color Optimization ---');
if (js.includes('rgba:`rgba(${tc.r[0]},${tc.r[1]},${tc.r[2]},`')) {
  pass('Nodes use pre-computed rgba prefix (no string concat per frame)');
} else {
  warn('rgba prefix may not be pre-computed');
}

// --- Viewport culling ---
console.log('\n--- Viewport Culling ---');
if (src.includes('vpLeft') && src.includes('vpRight') && src.includes('vpTop') && src.includes('vpBottom')) {
  pass('drawEdges: viewport culling with 4 bounds');
} else {
  fail('Viewport culling missing in drawEdges');
}
if (src.includes('sx<-50||sx>W+50||sy<-50||sy>H+50')) {
  pass('drawNodes: offscreen nodes skipped');
} else {
  fail('drawNodes viewport culling missing');
}

// --- Shape rendering ---
console.log('\n--- Node Shapes ---');
const shapes = ['diamond', 'triangle', 'star', 'hex', 'ring'];
for (const shape of shapes) {
  if (src.includes(`case'${shape}':`)) pass(`Shape '${shape}' rendered`);
  else fail(`Shape '${shape}' missing`);
}

// --- Curved edges ---
console.log('\n--- Edge Rendering ---');
if (src.includes('quadraticCurveTo')) {
  pass('Quadratic curve edges for high LOD');
} else {
  fail('No curved edges');
}
if (src.includes('useCurve=lod===\'high\'')) {
  pass('Curves only in high LOD (performance)');
} else {
  warn('Curve usage not gated by LOD');
}

// --- Particle clearing in low LOD ---
console.log('\n--- Particles ---');
if (src.includes("e.particles.length=0")) {
  pass('Particles cleared in low LOD');
} else {
  fail('Particles not cleared in low LOD');
}
if (src.includes('MAX_PARTICLES=60')) {
  pass('Ambient particles capped at 60');
} else {
  fail('No ambient particle cap');
}

// --- Minimap optimization ---
console.log('\n--- Minimap ---');
if (src.includes('frameCount%2===0)drawMinimap')) {
  pass('Minimap renders every other frame');
} else {
  warn('Minimap not optimized');
}

// --- 3D Check ---
console.log('\n--- 3D Rendering Check ---');
const hasWebGL = src.includes('webgl') || src.includes('WebGL') || src.includes('THREE.') || src.includes('getContext("webgl")');
if (hasWebGL) {
  fail('3D/WebGL references found in 2D file!');
} else {
  pass('No WebGL/Three.js references');
}
const has3DCSS = src.includes('translateZ') || src.includes('rotateX') || src.includes('rotateY') || src.includes('perspective') || src.includes('matrix3d');
if (has3DCSS) {
  fail('3D CSS transforms found!');
} else {
  pass('No 3D CSS transforms');
}

// --- Summary ---
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');
if (failed > 0) process.exit(1);
