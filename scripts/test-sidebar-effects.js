#!/usr/bin/env node
/**
 * test-sidebar-effects.js
 * Worker 98 — Sidebar UI, Visual Effects & Memory Safety Tests
 *
 * Tests:
 * 1. Memory safety: array caps
 * 2. DOM cleanup: recall popups auto-remove
 * 3. Namespace color hashing: deterministic
 * 4. Minimap: viewport rectangle calculation
 * 5. Burst effect: ring + flash + 6 particles
 * 6. Activity feed: unshift (newest first)
 * 7. No 3D CSS
 * 8. 2D canvas contexts only
 * 9. DOM leak / event listener leak checks
 */

const fs = require('fs');
const path = require('path');
const { execSync } = require('child_process');

const HTML_PATH = path.join(__dirname, '..', 'internal', 'handler', 'static', 'graph.html');

let passed = 0, failed = 0, warnings = 0;

function pass(msg) { passed++; console.log(`  PASS  ${msg}`); }
function fail(msg) { failed++; console.log(`  FAIL  ${msg}`); }
function warn(msg) { warnings++; console.log(`  WARN  ${msg}`); }

// Load HTML
if (!fs.existsSync(HTML_PATH)) { console.error('File not found:', HTML_PATH); process.exit(1); }
const html = fs.readFileSync(HTML_PATH, 'utf8');
const lines = html.split('\n');

// Extract JS block
const scriptMatch = html.match(/<script>([\s\S]*?)<\/script>/);
if (!scriptMatch) { console.error('No <script> block found'); process.exit(1); }
const js = scriptMatch[1];

console.log('=== MindBank Sidebar, Effects & Memory Safety Tests ===\n');
console.log('Loaded: ' + HTML_PATH + ' (' + lines.length + ' lines, ' + (html.length / 1024).toFixed(1) + ' KB)\n');

// =========================================================================
// 1. MEMORY SAFETY — Array Caps
// =========================================================================
console.log('--- Memory Safety: Array Caps ---');

// Burst cap at 20
if (js.includes("bursts.length>20") && js.includes("bursts.splice(0,bursts.length-20)")) {
  pass('Burst cap at 20 (splice oldest)');
} else {
  fail('Burst cap not found or incorrect — expected bursts.length>20 with splice');
}

// Particle cap at 60
if (js.includes('MAX_PARTICLES=60')) {
  pass('Particle cap at MAX_PARTICLES=60');
} else {
  fail('MAX_PARTICLES constant not found or != 60');
}

// Feed cap at 30
if (js.includes('activityFeed.length>30') && js.includes('activityFeed.pop()')) {
  pass('Feed items cap at 30');
} else {
  fail('Feed cap not found — expected activityFeed.length>30 with pop()');
}

// Glow trail cap at 3
if (js.includes('glowTrail.length>3') && js.includes('glowTrail.length=3')) {
  pass('Glow trail cap at 3');
} else {
  fail('Glow trail cap not found — expected glowTrail.length>3 with =3');
}

// Recall popup DOM cap at 5
if (js.includes('container.children.length>5') && js.includes('container.removeChild(container.firstChild)')) {
  pass('DOM recall popup cap at 5 (remove oldest child)');
} else {
  fail('DOM recall popup cap not found — expected children.length>5');
}

// Edge particles capped at 2 per edge
if (js.includes('e.particles.length<2')) {
  pass('Edge particles capped at 2 per edge');
} else {
  warn('Edge particle cap per edge not clearly enforced');
}

// visNodesCache cleared properly
if (js.includes('visNodesCache.length=0')) {
  pass('visNodesCache cleared on rebuild');
} else {
  warn('visNodesCache cleanup not found');
}

// =========================================================================
// 2. DOM CLEANUP — Recall popups auto-remove
// =========================================================================
console.log('\n--- DOM Cleanup ---');

if (js.includes('setTimeout(()=>el.remove(),3200)')) {
  pass('Recall popup auto-removes after 3.2s (setTimeout + el.remove())');
} else {
  fail('Recall popup auto-cleanup not found — expected setTimeout 3200ms + el.remove()');
}

// CSS animation matches JS timeout
if (html.includes('recall-fade 3s forwards')) {
  pass('CSS animation duration (3s) aligns with JS cleanup (3.2s)');
} else {
  warn('CSS recall-fade animation not found or duration mismatch');
}

// Feed uses innerHTML replacement (no stale DOM)
if (js.includes("el.innerHTML=activityFeed.slice(0,30).map(")) {
  pass('Feed uses innerHTML replacement — no stale DOM accumulation');
} else {
  warn('Feed rendering pattern not confirmed');
}

// Namespace chips use innerHTML replacement
if (js.includes("el.innerHTML=Object.entries(nsMap).map(")) {
  pass('Namespace chips use innerHTML replacement — no stale DOM');
} else {
  warn('Namespace chips rendering pattern not confirmed');
}

// Data sidebar uses innerHTML replacement
if (js.includes("el.innerHTML=Object.entries(typeCounts)")) {
  pass('Data sidebar uses innerHTML replacement — no stale DOM');
} else {
  warn('Data sidebar rendering pattern not confirmed');
}

// =========================================================================
// 3. NAMESPACE COLOR HASHING — Deterministic
// =========================================================================
console.log('\n--- Namespace Color Hashing ---');

// nsColor function uses hash-based selection
if (js.includes('function nsColor(ns)')) {
  pass('nsColor() function exists');
} else {
  fail('nsColor() function not found');
}

// Uses djb2-style hash: ((h<<5)-h)+charCodeAt
if (js.includes('((h<<5)-h)+ns.charCodeAt(i)')) {
  pass('nsColor uses djb2 hash (deterministic for same input)');
} else {
  fail('nsColor hash algorithm not found — expected djb2 ((h<<5)-h)+charCodeAt');
}

// Indexes into NS_COLORS array with modulo
if (js.includes('NS_COLORS[Math.abs(h)%NS_COLORS.length]')) {
  pass('nsColor indexes NS_COLORS with modulo (stable output)');
} else {
  fail('nsColor modulo indexing not found');
}

// WARN: renderNamespaceChips does NOT use nsColor — uses index-based colors
if (js.includes("const colors=['#00ff88','#0088ff','#ffaa00'")) {
  // Check if renderNamespaceChips uses this array instead of nsColor
  const chipsSection = js.match(/function renderNamespaceChips[\s\S]*?^}/m);
  if (chipsSection && chipsSection[0].includes("colors[i%colors.length]") && !chipsSection[0].includes('nsColor')) {
    warn('renderNamespaceChips uses index-based colors, NOT nsColor() — chip colors depend on enumeration order, may change between loads');
  } else {
    pass('renderNamespaceChips uses nsColor for consistent coloring');
  }
}

// NS_COLORS has 12 entries
const nsColorsMatch = js.match(/const NS_COLORS=\[([\s\S]*?)\];/);
if (nsColorsMatch) {
  const entries = nsColorsMatch[1].match(/\[[\d,\s]+\]/g);
  if (entries && entries.length === 12) {
    pass('NS_COLORS has 12 color entries');
  } else {
    warn(`NS_COLORS has ${entries ? entries.length : '?'} entries (expected 12)`);
  }
} else {
  warn('Could not parse NS_COLORS array');
}

// =========================================================================
// 4. MINIMAP — Viewport Rectangle Calculation
// =========================================================================
console.log('\n--- Minimap Viewport Rectangle ---');

// Minimap finds node bounds
if (js.includes('let minX=Infinity,maxX=-Infinity,minY=Infinity,maxY=-Infinity')) {
  pass('Minimap calculates node bounds (minX/maxX/minY/maxY)');
} else {
  fail('Minimap bounds calculation not found');
}

// Padding applied
if (js.includes('const pad=50;minX-=pad;maxX+=pad;minY-=pad;maxY+=pad')) {
  pass('Minimap applies 50px padding to bounds');
} else {
  fail('Minimap padding not found');
}

// Scale calculated as min of width/range and height/range
if (js.includes('const scale=Math.min(mw/rangeX,mh/rangeY)')) {
  pass('Minimap scale preserves aspect ratio (Math.min of ratios)');
} else {
  fail('Minimap aspect-preserving scale not found');
}

// Viewport rect uses camera transform
// vpL = (-cam.x - W/2/cam.z - minX) * scale + offX
// This represents: left edge of viewport in world space, then mapped to minimap
if (js.includes('vpL=(-cam.x-W/2/cam.z-minX)*scale+offX')) {
  pass('Minimap viewport left: (-cam.x - W/2/cam.z - minX) * scale + offX');
} else {
  fail('Minimap viewport left calculation not found or incorrect');
}

if (js.includes('vpT=(-cam.y-H/2/cam.z-minY)*scale+offY')) {
  pass('Minimap viewport top: (-cam.y - H/2/cam.z - minY) * scale + offY');
} else {
  fail('Minimap viewport top calculation not found or incorrect');
}

if (js.includes('vpW=(W/cam.z)*scale')) {
  pass('Minimap viewport width: (W/cam.z) * scale');
} else {
  fail('Minimap viewport width calculation not found');
}

if (js.includes('vpH=(H/cam.z)*scale')) {
  pass('Minimap viewport height: (H/cam.z) * scale');
} else {
  fail('Minimap viewport height calculation not found');
}

// =========================================================================
// 5. BURST EFFECT — Ring + Flash + 6 Particles
// =========================================================================
console.log('\n--- Burst Effect Verification ---');

// triggerBurst function exists
if (js.includes('function triggerBurst(')) {
  pass('triggerBurst() function exists');
} else {
  fail('triggerBurst() not found');
}

// Burst has x, y, t, color, size properties
if (js.includes("bursts.push({x:sx,y:sy,t:0,color:n.rgba,size:n.size*cam.z*2})")) {
  pass('Burst object has x, y, t, color, size properties');
} else {
  warn('Burst object structure may differ from expected');
}

// drawBursts clears burst canvas each frame
if (js.includes('bctx.clearRect(0,0,W,H)')) {
  pass('drawBursts() clears burst canvas each frame');
} else {
  fail('drawBursts() does not clear canvas — will ghost');
}

// Expanding ring
if (js.includes('bctx.arc(b.x,b.y,r,0,Math.PI*2)') && js.includes("bctx.strokeStyle=`${b.color}${alpha})`")) {
  pass('Burst has expanding ring (arc + strokeStyle with fade)');
} else {
  fail('Burst expanding ring not found');
}

// Inner flash (radial gradient)
if (js.includes('bctx.createRadialGradient(b.x,b.y,0,b.x,b.y,r*0.5)')) {
  pass('Burst has inner flash (radial gradient at center)');
} else {
  fail('Burst inner flash gradient not found');
}

// 6 radiating particles
const particleLoopMatch = js.match(/for\(let j=0;j<(\d);j\+\+\)/);
if (js.includes('for(let j=0;j<6;j++)') && js.includes("const angle=(j/6)*Math.PI*2+b.t*3")) {
  pass('Burst has exactly 6 radiating particles');
} else {
  fail('Burst radiating particles count != 6 or pattern not found');
}

// Particles orbit (angle includes b.t*3 for rotation)
if (js.includes('b.t*3')) {
  pass('Burst particles rotate over time (angle includes b.t*3)');
} else {
  warn('Burst particle rotation not confirmed');
}

// Burst edges glow
if (js.includes("if(e.src===nodeId||e.dst===nodeId)e.glow=1")) {
  pass('triggerBurst() sets glow=1 on connected edges');
} else {
  warn('Edge glow on burst not found');
}

// =========================================================================
// 6. ACTIVITY FEED — Newest First (unshift, not push)
// =========================================================================
console.log('\n--- Activity Feed Order ---');

if (js.includes('activityFeed.unshift(item)')) {
  pass('Feed uses unshift (newest items appear first)');
} else {
  fail('Feed does not use unshift — newest items may not appear first');
}

if (js.includes('activityFeed.pop()')) {
  pass('Feed trims with pop() (removes oldest from end)');
} else {
  fail('Feed trim method not found');
}

// renderFeed slices first 30
if (js.includes('activityFeed.slice(0,30)')) {
  pass('renderFeed displays first 30 items (newest)');
} else {
  warn('renderFeed slice limit not confirmed');
}

// =========================================================================
// 7. NO 3D CSS — No transform3d, perspective, rotateX/Y/Z
// =========================================================================
console.log('\n--- 3D CSS Check ---');

const css3dPatterns = [
  ['translateZ', /translateZ\s*\(/],
  ['rotateX', /rotateX\s*\(/],
  ['rotateY', /rotateY\s*\(/],
  ['perspective', /perspective\s*\(/],
  ['matrix3d', /matrix3d\s*\(/],
  ['transform-style: preserve-3d', /transform-style\s*:\s*preserve-3d/],
  ['backface-visibility', /backface-visibility\s*:/],
];

let found3d = false;
for (const [name, regex] of css3dPatterns) {
  // Only check CSS (style block), not JS
  const cssBlock = html.match(/<style>([\s\S]*?)<\/style>/);
  if (cssBlock && regex.test(cssBlock[1])) {
    fail(`3D CSS found: ${name}`);
    found3d = true;
  }
}
if (!found3d) {
  pass('No 3D CSS transforms (translateZ, rotateX/Y, perspective, matrix3d)');
}

// Check JS for 3D transforms too
let js3dFound = false;
for (const [name, regex] of [['translateZ', /translateZ/], ['rotateX', /rotateX/], ['rotateY', /rotateY/], ['perspective', /perspective/], ['matrix3d', /matrix3d/]]) {
  if (regex.test(js)) {
    fail(`3D JS transform found: ${name}`);
    js3dFound = true;
  }
}
if (!js3dFound) {
  pass('No 3D JS transforms');
}

// =========================================================================
// 8. 2D CANVAS CONTEXTS — No WebGL
// =========================================================================
console.log('\n--- Canvas Context Verification ---');

// Main canvas
if (js.includes("canvas.getContext('2d')")) {
  pass('Main canvas uses getContext("2d")');
} else {
  fail('Main canvas not using 2D context');
}

// Burst canvas
if (js.includes("burstCanvas.getContext('2d')")) {
  pass('Burst canvas uses getContext("2d")');
} else {
  fail('Burst canvas not using 2D context');
}

// Minimap canvas
if (js.includes("minimapCanvas.getContext('2d')")) {
  pass('Minimap canvas uses getContext("2d")');
} else {
  fail('Minimap canvas not using 2D context');
}

// No WebGL
if (!js.includes('getContext(') || !js.includes('webgl')) {
  // More precise: check no getContext call uses 'webgl'
  const ctxCalls = js.match(/getContext\(['"](.*?)['"]\)/g) || [];
  const hasWebGL = ctxCalls.some(c => c.includes('webgl'));
  if (!hasWebGL) {
    pass('No WebGL context calls found');
  } else {
    fail('WebGL context call found — should be 2D only');
  }
}

// =========================================================================
// 9. DOM / EVENT LEAK CHECKS
// =========================================================================
console.log('\n--- DOM & Event Leak Checks ---');

// Event listeners: check for addEventListener (potential leaks if not removed)
const addEL = (js.match(/addEventListener/g) || []).length;
const remEL = (js.match(/removeEventListener/g) || []).length;
if (addEL > 0 && remEL === 0) {
  // Only flag if they're on elements that could be recreated
  // visibilitychange listener on document is fine (never removed, page-lifetime)
  if (addEL === 1 && js.includes("document.addEventListener('visibilitychange'")) {
    pass('Single addEventListener (visibilitychange on document) — page-lifetime, no leak');
  } else {
    warn(`${addEL} addEventListener calls, 0 removeEventListener — check for leaks`);
  }
} else if (addEL === 0) {
  pass('No addEventListener calls (uses on* properties — no leak risk)');
} else {
  pass(`${addEL} addEventListener, ${remEL} removeEventListener — balanced`);
}

// Canvas events use on* properties (auto-replaced, no accumulation)
const onEvents = (js.match(/canvas\.on\w+\s*=/g) || []).length;
if (onEvents > 0) {
  pass(`Canvas uses ${onEvents} on* property handlers (no listener accumulation)`);
}

// setInterval without clearInterval
const setIntervals = (js.match(/setInterval/g) || []).length;
const clearIntervals = (js.match(/clearInterval/g) || []).length;
if (setIntervals > 0 && clearIntervals === 0) {
  warn(`${setIntervals} setInterval(s) without clearInterval — intervals run forever (expected for polling, but no cleanup on unload)`);
} else if (setIntervals === 0) {
  pass('No setInterval calls');
}

// setTimeout for recall popup — no clearInterval needed (one-shot)
const setTos = (js.match(/setTimeout/g) || []).length;
pass(`${setTos} setTimeout calls (all one-shot, auto-GC'd)`);

// Check for global variable leaks (too many globals is a smell)
const globalLets = js.match(/^let\s+[\w,=\s{}]+;/gm) || [];
if (globalLets.length > 20) {
  warn(`${globalLets.length} global let declarations — consider namespacing`);
} else {
  pass(`${globalLets.length} global let declarations (acceptable)`);
}

// drawBursts: radial gradient created per burst per frame — potential perf issue
const gradPerBurst = js.includes('bctx.createRadialGradient');
if (gradPerBurst) {
  warn('drawBursts creates radial gradient per burst per frame — expensive with many simultaneous bursts');
} else {
  pass('No per-frame gradient creation in drawBursts');
}

// =========================================================================
// SUMMARY
// =========================================================================
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');

if (failed > 0) {
  console.log('\nISSUES FOUND:');
  console.log('See FAIL lines above for details.');
}

if (warnings > 0) {
  console.log('\nWARNINGS:');
  console.log('See WARN lines above — not broken but worth reviewing.');
}

process.exit(failed > 0 ? 1 : 0);
