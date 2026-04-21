#!/usr/bin/env node
/**
 * Interaction & Event Handling Deep Review — Worker 97 replacement
 * Tests camera, coordinate transforms, click detection, node selection.
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

console.log('=== Interaction & Event Handling Review ===\n');

// --- Camera System ---
console.log('--- Camera ---');
if (src.includes('cam={x:0,y:0,z:1}')) {
  pass('Camera init: 2D (x, y, z=zoom only)');
} else {
  fail('Camera init not found or wrong format');
}
if (src.includes('function resetCam()')) pass('resetCam() exists');
if (src.includes('function zoomFit()')) pass('zoomFit() exists');
if (src.includes('autoR')) pass('Auto-rotate toggle exists');

// --- Coordinate Transform ---
console.log('\n--- Coordinate Transform ---');
// World to screen: (n.x - cam.x) * cam.z + cx
const worldToScreen = src.match(/\(n\.x-cam\.x\)\*cam\.z\+cx/);
if (worldToScreen) {
  pass('World→Screen: (n.x-cam.x)*cam.z + center');
} else {
  fail('World→Screen transform not found');
}
// Check it's purely 2D (no z-depth, no perspective)
const transformBlock = src.substring(src.indexOf('const sx=(n.x'), src.indexOf('const sx=(n.x') + 100);
if (transformBlock.includes('perspective') || transformBlock.includes('fov') || transformBlock.includes('depth')) {
  fail('3D perspective projection found in coordinate transform!');
} else {
  pass('Coordinate transform is pure 2D (no perspective/depth)');
}

// --- Click / Hover Detection ---
console.log('\n--- Click Detection ---');
if (src.includes('Math.sqrt(dx*dx+dy*dy)') || src.includes('dx=mx-n.x,dy=my-n.y')) {
  pass('Distance-based hit detection found');
} else {
  warn('Hit detection method unclear');
}
// Check screen coordinates used correctly
if (src.includes('mx=') && src.includes('my=') && src.includes('getBoundingClientRect')) {
  pass('Mouse coords use getBoundingClientRect (correct for embed)');
} else if (src.includes('e.clientX') && src.includes('e.clientY')) {
  pass('Mouse coords use clientX/clientY');
} else {
  warn('Mouse coordinate handling unclear');
}

// --- Node Selection ---
console.log('\n--- Node Selection ---');
if (src.includes('function selectNodeForHighlight')) {
  pass('selectNodeForHighlight() exists');
  // Check that connected nodes are found
  if (src.includes('connectedSet=new Set([nid])') && src.includes('connectedSet.add(e.dst)')) {
    pass('Selection builds connected set correctly');
  } else {
    warn('Connected set construction unclear');
  }
}
if (src.includes('selectedNode===nid')) {
  pass('Toggle: clicking selected node deselects');
} else {
  warn('No toggle deselect on re-click');
}

// --- Dimming Non-Connected ---
console.log('\n--- Visual Feedback ---');
if (src.includes('connectedSet&&!connectedSet.has(n.id)') && src.includes('nodeAlpha=0.15')) {
  pass('Non-connected nodes dimmed to alpha=0.15');
} else {
  fail('Selection dimming not found or wrong alpha');
}

// --- Search / Filter ---
console.log('\n--- Search & Filter ---');
if (src.includes('function filterNodes')) pass('filterNodes() exists');
if (src.includes('function setNSFilter')) pass('setNSFilter() exists');
if (src.includes('function filterByType')) pass('filterByType() exists');

// --- Embed Mode ---
console.log('\n--- Embed Mode ---');
if (src.includes("get('embed')")) {
  pass('Embed mode query param detected');
} else {
  fail('No embed mode detection');
}
if (src.includes('embed-mode')) {
  pass('embed-mode CSS class applied');
} else {
  fail('No embed-mode CSS class');
}

// --- 3D Interaction Check ---
console.log('\n--- 3D Interaction Check ---');
const has3D = src.includes('orbit') || src.includes('trackball') || src.includes('perspective') || src.includes('raycaster') || src.includes('project(');
if (has3D) {
  fail('3D interaction patterns found!');
} else {
  pass('No 3D interaction patterns (orbit, trackball, raycaster)');
}

// --- Drag Handling ---
console.log('\n--- Drag Handling ---');
if (src.includes('drag=true') && src.includes('drag=false')) {
  pass('Drag state toggle exists');
} else {
  warn('Drag state handling unclear');
}
if (src.includes('camStart') && src.includes('dragStart')) {
  pass('Drag uses separate start positions for cam and mouse');
} else {
  warn('Drag start position tracking unclear');
}

// --- Summary ---
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');
if (failed > 0) process.exit(1);
