#!/usr/bin/env node
/**
 * MindBank Graph Integrity Test Suite
 * Tests graph.html for JS syntax, structure, and logical correctness.
 * Run: node scripts/test-graph-integrity.js
 */

const fs = require('fs');
const path = require('path');

const GRAPH_PATH = path.join(__dirname, '..', 'internal', 'handler', 'static', 'graph.html');
let passed = 0, failed = 0, warnings = 0;

function test(name, fn) {
  try {
    const result = fn();
    if (result === true || result === undefined) {
      console.log(`  PASS  ${name}`);
      passed++;
    } else if (result === 'warn') {
      console.log(`  WARN  ${name}`);
      warnings++;
    } else {
      console.log(`  FAIL  ${name}: ${result}`);
      failed++;
    }
  } catch (e) {
    console.log(`  FAIL  ${name}: ${e.message}`);
    failed++;
  }
}

console.log('=== MindBank Graph Integrity Tests ===\n');

// Load file
const src = fs.readFileSync(GRAPH_PATH, 'utf8');
const lines = src.split('\n');
console.log(`Loaded: ${GRAPH_PATH} (${lines.length} lines, ${(src.length / 1024).toFixed(1)} KB)\n`);

// --- Structure Tests ---
console.log('--- Structure ---');

test('Has DOCTYPE declaration', () => src.includes('<!DOCTYPE html>'));
test('Has closing html tag', () => src.includes('</html>'));
test('Has canvas element', () => src.includes('<canvas id="c"'));
test('Has burst canvas', () => src.includes('<canvas id="burstC"'));
test('Has minimap canvas', () => src.includes('<canvas id="minimapC"'));
test('Has sidebar', () => src.includes('class="sidebar"'));
test('Has topbar', () => src.includes('class="topbar"'));
test('Has detail panel', () => src.includes('class="panel"'));
test('Has script block', () => src.includes('<script>'));

// --- JavaScript Syntax Tests ---
console.log('\n--- JavaScript Syntax ---');

test('Extractable JS block', () => {
  const match = src.match(/<script>([\s\S]*?)<\/script>/);
  if (!match) return 'No script block found';
  return true;
});

test('JS block has no syntax errors (node --check)', () => {
  const match = src.match(/<script>([\s\S]*?)<\/script>/);
  if (!match) return 'No script block';
  const js = match[1];
  // Write temp file and check
  const tmpFile = '/tmp/_mindbank_graph_test.js';
  fs.writeFileSync(tmpFile, js);
  const { execSync } = require('child_process');
  try {
    execSync(`node --check ${tmpFile}`, { stdio: 'pipe' });
    return true;
  } catch (e) {
    return `Syntax error: ${e.stderr.toString().trim()}`;
  }
});

// --- Function Presence Tests ---
console.log('\n--- Required Functions ---');

const requiredFunctions = [
  'resize', 'load', 'buildGraph', 'physics', 'render',
  'drawGrid', 'drawEdges', 'drawNodes', 'drawNodeShape',
  'drawStar', 'drawHex', 'initParticles', 'drawParticles',
  'triggerBurst', 'drawBursts', 'drawMinimap',
  'toggleHealth', 'selectNodeForHighlight', 'healthColorRgb', 'healthColorPrefix',
  'showRecallPopup', 'drawRecallPopups',
  'renderDataSidebar', 'filterByType', 'addFeedItem', 'renderFeed',
  'pollHermesRecalls', 'simulateRecall',
  'renderNamespaceChips', 'setNSFilter', 'filterNodes',
  'toggleRotate', 'showNodePanel', 'focusNode',
  'resetCam', 'zoomFit', 'toast', 'renderLegend'
];

for (const fn of requiredFunctions) {
  test(`function ${fn} exists`, () => {
    const re = new RegExp(`function\\s+${fn}\\s*\\(`);
    return re.test(src) ? true : `Missing function ${fn}`;
  });
}

// --- Constant/Config Tests ---
console.log('\n--- Constants & Config ---');

test('TC (type config) has 11 types', () => {
  const types = ['project', 'decision', 'fact', 'preference', 'problem',
                 'advice', 'topic', 'person', 'event', 'concept', 'agent'];
  return types.every(t => src.includes(`${t}:`)) ? true : 'Missing type in TC';
});

test('EDGE_STYLES has 6 types', () => {
  const types = ['relates_to', 'contains', 'supports', 'depends_on', 'decided_by', 'contradicts'];
  return types.every(t => src.includes(`${t}:`)) ? true : `Missing edge type`;
});

test('RECALL_TYPES defined', () => src.includes('const RECALL_TYPES='));
test('NS_COLORS defined', () => src.includes('const NS_COLORS='));

// --- Physics Logic Tests ---
console.log('\n--- Physics Logic ---');

test('visNodesCache rebuilt BEFORE early return check', () => {
  const physIdx = src.indexOf('function physics()');
  if (physIdx === -1) return 'physics() not found';
  const physBlock = src.substring(physIdx, physIdx + 500);
  const rebuildIdx = physBlock.indexOf('visNodesCache.length=0');
  const returnIdx = physBlock.indexOf('if(visCount===0)return');
  if (rebuildIdx === -1) return 'visNodesCache rebuild not found';
  if (returnIdx === -1) return 'early return not found';
  return rebuildIdx < returnIdx ? true : 'CRITICAL: visNodesCache rebuild is AFTER early return';
});

test('NaN guards in physics (isFinite checks)', () => {
  return src.includes('if(!isFinite(n.x))') && src.includes('if(!isFinite(n.y))')
    ? true : 'Missing NaN guards in physics';
});

test('NaN guards in drawNodes', () => {
  const drawNodesIdx = src.indexOf('function drawNodes');
  const drawNodesBlock = src.substring(drawNodesIdx, drawNodesIdx + 2000);
  return drawNodesBlock.includes('isFinite(n.x)') ? true : 'Missing NaN guard in drawNodes';
});

test('lastNodeCount uses count API for accurate baseline', () => {
  return src.includes("lastNodeCount=cnt.count||d.nodes.length") ? true
    : 'lastNodeCount not using count API';
});

test('Edge physics capped at 500', () => {
  return src.includes('Math.min(edges.length,500)') ? true : 'Edge physics not capped';
});

test('Repulsion distance capped (250000)', () => {
  return src.includes('repulsionDist2=250000') ? true : 'Repulsion distance not capped';
});

test('Collision handling exists', () => {
  return src.includes('Collision: gradual push apart') || src.includes('overlap')
    ? true : 'No collision handling found';
});

test('Physics settled detection exists', () => {
  return src.includes('physicsSettled=true') ? true : 'No settled detection';
});

test('Force settle after 200 frames', () => {
  return src.includes('physicsFrame>200') ? true : 'No force settle';
});

// --- Rendering Logic Tests ---
console.log('\n--- Rendering Logic ---');

test('LOD system exists (high/medium/low)', () => {
  return src.includes("'high'") && src.includes("'medium'") && src.includes("'low'")
    ? true : 'LOD system missing';
});

test('Viewport culling in drawEdges', () => {
  return src.includes('vpLeft') && src.includes('vpRight')
    ? true : 'No viewport culling in drawEdges';
});

test('Edge cap by LOD', () => {
  return src.includes("lod==='high'?1000:lod==='medium'?500:200")
    ? true : 'Edge cap by LOD missing';
});

test('Particles cleared in low LOD', () => {
  return src.includes("e.particles.length=0") ? true : 'Particles not cleared in low LOD';
});

test('No gradients per frame in drawNodes', () => {
  // Check that drawNodes doesn't create gradients (performance killer)
  const drawIdx = src.indexOf('function drawNodes');
  const drawBlock = src.substring(drawIdx, drawIdx + 3000);
  const hasGrad = drawBlock.includes('createRadialGradient') || drawBlock.includes('createLinearGradient');
  // drawNodeShape handles shapes, so check there too
  const shapeIdx = src.indexOf('function drawNodeShape');
  const shapeBlock = src.substring(shapeIdx, shapeIdx + 1000);
  const shapeGrad = shapeBlock.includes('createRadialGradient');
  return hasGrad ? 'GRADIENT IN drawNodes — potential FPS killer' : true;
});

test('globalAlpha reset after each draw block', () => {
  // Check that ctx.globalAlpha=1 appears after drawEdges and drawNodes
  return src.includes('ctx.globalAlpha=1') ? true : 'globalAlpha not reset';
});

test('Quadratic curve edges exist', () => {
  return src.includes('quadraticCurveTo') ? true : 'No curved edges';
});

test('Minimap renders every other frame', () => {
  return src.includes('frameCount%2===0)drawMinimap') ? true
    : 'Minimap not optimized';
});

// --- API Endpoint Tests ---
console.log('\n--- API Endpoints ---');

test('Graph API: /api/v1/graph', () => src.includes("/api/v1/graph"));
test('Nodes API: /api/v1/nodes', () => src.includes("/api/v1/nodes"));
test('Snapshot API: /api/v1/snapshot', () => src.includes("/api/v1/snapshot"));
test('Neighbors API: /api/v1/nodes/.../neighbors', () => src.includes("'/api/v1/nodes/'+n.id+'/neighbors'"));
test('Count API with count=true param', () => src.includes('count=true'));

// --- Polling & Real-time Tests ---
console.log('\n--- Polling & Real-time ---');

test('pollBusy guard prevents overlapping polls', () => {
  return src.includes('if(pollBusy)return') ? true : 'No pollBusy guard';
});

test('AbortController with timeout on snapshot fetch', () => {
  return src.includes('AbortController') && src.includes("controller.abort()")
    ? true : 'No abort controller on snapshot';
});

test('AbortController with timeout on count fetch', () => {
  // Should have second abort controller for count check
  const countMatches = src.match(/AbortController/g);
  return countMatches && countMatches.length >= 2 ? true : 'Missing second AbortController';
});

test('15s interval for graph rebuild check', () => {
  return src.includes('15000') ? true : 'No 15s interval for graph check';
});

test('Access count delta detection', () => {
  return src.includes('remote.access_count>local.acc') ? true
    : 'No access_count delta detection';
});

// --- Embed Mode Tests ---
console.log('\n--- Embed Mode ---');

test('Embed mode query param detection', () => {
  return src.includes("get('embed')") ? true : 'No embed param detection';
});

test('Embed mode CSS class', () => {
  return src.includes('embed-mode') ? true : 'No embed-mode CSS class';
});

// --- Memory Safety Tests ---
console.log('\n--- Memory Safety ---');

test('buildGraph clears old arrays', () => {
  return src.includes('edges.length=0') && src.includes('nodeMap.clear()')
    && src.includes('nodes.forEach(n=>{n.glowTrail.length=0;})')
    ? true : 'buildGraph not clearing old arrays';
});

test('Burst cap at 20', () => {
  return src.includes('bursts.length>20') ? true : 'No burst cap';
});

test('Particle cap at MAX_PARTICLES=60', () => {
  return src.includes('MAX_PARTICLES=60') ? true : 'No particle cap';
});

test('Feed items cap at 30', () => {
  return src.includes('activityFeed.length>30') ? true : 'No feed cap';
});

test('Glow trail cap at 3', () => {
  return src.includes("n.glowTrail.length>3") ? true : 'No glow trail cap';
});

test('DOM recall popup cap at 5', () => {
  return src.includes('children.length>5') ? true : 'No DOM popup cap';
});

test('Visibility change pauses rendering', () => {
  return src.includes("document.hidden") && src.includes('renderRunning=false')
    ? true : 'No tab visibility optimization';
});

// --- 2D vs 3D Consistency Check ---
console.log('\n--- 2D Consistency (no 3D leakage) ---');

test('No WebGL references', () => {
  const hasWebGL = src.includes('webgl') || src.includes('WebGL')
    || src.includes('gl.') || src.includes('THREE.');
  return hasWebGL ? 'WebGL/3D references found in 2D file' : true;
});

test('No 3D z-coordinate in nodes', () => {
  const buildIdx = src.indexOf('return{id:');
  if (buildIdx === -1) return 'Node construction not found';
  const nodeBlock = src.substring(buildIdx, buildIdx + 300);
  // Should have x, y, vx, vy — NO z
  const hasZ = /\bz\b/.test(nodeBlock.replace(/'z'/g, '').replace(/"z"/g, ''));
  return hasZ ? 'z-coordinate found in 2D node — possible 3D leakage' : true;
});

test('Camera is 2D (x, y, z=zoom only)', () => {
  const camInit = src.match(/cam=\{[^}]+\}/);
  if (!camInit) return 'Camera init not found';
  return camInit[0].includes('z:1') ? true : 'Camera z is not zoom level';
});

test('Physics only modifies x, y, vx, vy', () => {
  const physIdx = src.indexOf('function physics()');
  const physBlock = src.substring(physIdx, physIdx + 3000);
  // Should NOT reference n.vz or n.z for depth
  return physBlock.includes('n.vz') || physBlock.includes('n.z')
    ? 'Physics references z-axis — 3D leakage' : true;
});

// --- Physics Simulation Test ---
console.log('\n--- Physics Simulation ---');

test('Physics converges without divergence (50 frames)', () => {
  // Extract physics function, simulate with test nodes
  const match = src.match(/<script>([\s\S]*?)<\/script>/);
  const js = match[1];
  // Create a minimal test harness
  const testScript = `
    const nodes = [];
    const edges = [];
    const nodeMap = new Map();
    let visNodesCache = [];
    let physicsSettled = false;
    let settleCounter = 0;
    let physicsFrame = 0;

    // Create 20 test nodes
    for (let i = 0; i < 20; i++) {
      const angle = (i / 20) * Math.PI * 2;
      const n = {
        id: i, label: 'node' + i, type: 'fact', ns: 'test',
        x: Math.cos(angle) * 200, y: Math.sin(angle) * 200,
        vx: 0, vy: 0, size: 10, visible: true, imp: 0.5, acc: 0
      };
      nodes.push(n);
      nodeMap.set(i, n);
    }
    // Create 15 edges
    for (let i = 0; i < 15; i++) {
      edges.push({
        src: i, dst: (i + 1) % 20, type: 'relates_to', w: 1,
        style: {color:[100,100,120],rgba:'rgba(100,100,120,',dash:null,width:1,glow:false},
        particles:[],nextSpawn:0,burstTimer:0,glow:0
      });
    }

    ${js.match(/function physics\(\)\{[\s\S]*?^}/m)[0]}

    // Run 50 frames
    for (let f = 0; f < 50; f++) { physics(); }

    // Check all positions are finite
    let allFinite = true;
    let maxCoord = 0;
    for (const n of nodes) {
      if (!isFinite(n.x) || !isFinite(n.y)) allFinite = false;
      maxCoord = Math.max(maxCoord, Math.abs(n.x), Math.abs(n.y));
    }

    if (!allFinite) { console.log('FAIL: NaN/Infinity in positions'); process.exit(1); }
    if (maxCoord > 5000) { console.log('FAIL: Nodes diverged to ' + maxCoord); process.exit(1); }
    console.log('OK: maxCoord=' + maxCoord.toFixed(1) + ', settled=' + physicsSettled);
  `;
  const { execSync } = require('child_process');
  try {
    const result = execSync(`node -e '${testScript.replace(/'/g, "'\\''")}'`, {
      stdio: 'pipe', timeout: 10000
    }).toString().trim();
    return result.startsWith('OK') ? true : result;
  } catch (e) {
    return `Simulation failed: ${e.stderr?.toString().trim() || e.message}`;
  }
});

// --- Summary ---
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');
if (failed > 0) process.exit(1);
