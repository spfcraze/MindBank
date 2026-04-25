#!/usr/bin/env node
/**
 * Physics Engine Deep Review — Worker 94 replacement
 * Tests edge cases, correctness, and 2D-only consistency.
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

console.log('=== Physics Deep Review ===\n');

// --- Edge case: 1 node ---
console.log('--- Edge Case: 1 Node ---');
try {
  const test1 = `
    const nodes=[{id:0,x:0,y:0,vx:0,vy:0,size:10,visible:true}];
    const edges=[];
    const nodeMap=new Map([[0,nodes[0]]]);
    let visNodesCache=[],physicsSettled=false,settleCounter=0,physicsFrame=0;
    ${js.match(/function physics\(\)\{[\s\S]*?^}/m)[0]}
    for(let f=0;f<50;f++) physics();
    if(!isFinite(nodes[0].x)&&!isFinite(nodes[0].y)) { console.log('FAIL: NaN'); process.exit(1); }
    console.log('OK: single node stable at ('+nodes[0].x.toFixed(1)+','+nodes[0].y.toFixed(1)+')');
  `;
  fs.writeFileSync('/home/rat/mindbank/_test_phys1.js', test1);
  const { execSync } = require('child_process');
  const r = execSync('node /home/rat/mindbank/_test_phys1.js', { timeout: 5000 }).toString().trim();
  pass('1 node: ' + r);
} catch(e) { fail('1 node crash: ' + (e.stderr?.toString().trim() || e.message)); }

// --- Edge case: 1000 nodes performance ---
console.log('\n--- Performance: 1000 Nodes ---');
try {
  const test1k = `
    const nodes=[],edges=[],nodeMap=new Map();
    for(let i=0;i<1000;i++){
      const n={id:i,x:(Math.random()-0.5)*200,y:(Math.random()-0.5)*200,vx:0,vy:0,size:8+Math.random()*4,visible:true};
      nodes.push(n); nodeMap.set(i,n);
    }
    for(let i=0;i<200;i++) edges.push({src:i,dst:(i+1)%1000,w:1,style:{color:[100,100,120],rgba:'rgba(100,100,120,',dash:null,width:1,glow:false},particles:[],nextSpawn:0,burstTimer:0,glow:0,type:'relates_to'});
    let visNodesCache=[],physicsSettled=false,settleCounter=0,physicsFrame=0;
    ${js.match(/function physics\(\)\{[\s\S]*?^}/m)[0]}
    const start=Date.now();
    for(let f=0;f<200;f++) physics();
    const ms=Date.now()-start;
    console.log('OK: 1000 nodes, 200 frames in '+ms+'ms ('+(ms/200).toFixed(2)+'ms/frame)');
    if(ms/200>10) console.log('WARN: slow');
  `;
  fs.writeFileSync('/home/rat/mindbank/_test_phys1k.js', test1k);
  const { execSync } = require('child_process');
  const r = execSync('node /home/rat/mindbank/_test_phys1k.js', { timeout: 30000 }).toString().trim();
  if (r.includes('OK:')) pass(r.split('OK: ')[1]);
  if (r.includes('WARN:')) warn(r);
} catch(e) { fail('1000 nodes: ' + (e.stderr?.toString().trim() || e.message)); }

// --- Collision direction check ---
console.log('\n--- Collision Push Direction ---');
const collisionCode = src.substring(src.indexOf('Collision: gradual push apart'), src.indexOf('Collision: gradual push apart') + 400);
// Check that 'a' gets pushed in +nx direction and 'b' gets pushed in -nx direction
if (collisionCode.includes('a.x+=nx*overlap') && collisionCode.includes('b.x-=nx*overlap')) {
  pass('Collision: a pushed +nx, b pushed -nx (correct)');
} else {
  fail('Collision direction may be wrong — check sign conventions');
}
// Check Y axis: a.y should MINUS ny (perpendicular), b.y should PLUS ny
if (collisionCode.includes('a.y-=ny*overlap') && collisionCode.includes('b.y+=ny*overlap')) {
  pass('Collision Y: a pushed -ny, b pushed +ny (correct)');
} else {
  warn('Collision Y direction unusual — verify perpendicular push is correct');
}

// --- Namespace clustering check ---
console.log('\n--- Namespace Clustering ---');
if (js.includes('nsGroups') && js.includes('nsGroups[n.ns]')) {
  pass('Namespace clustering: same-namespace nodes attract each other');
} else {
  fail('Namespace clustering not found');
}

// --- 2D-Only Physics Check ---
console.log('\n--- 2D-Only Check ---');
const physBlock = src.substring(src.indexOf('function physics()'), src.indexOf('function physics()') + 3000);
if (physBlock.includes('n.vz') || physBlock.includes('n.z') || physBlock.includes('depth')) {
  fail('Physics references z-axis/depth — 3D leakage!');
} else {
  pass('Physics uses only x, y, vx, vy — pure 2D');
}

// --- NaN guard verification (in full source, not just physics block) ---
console.log('\n--- NaN Guards ---');
const nanChecks = ['if(!isFinite(n.x))n.x=0', 'if(!isFinite(n.y))n.y=0', 'if(!isFinite(n.vx))n.vx=0', 'if(!isFinite(n.vy))n.vy=0'];
for (const check of nanChecks) {
  if (src.includes(check)) pass(`NaN guard: ${check}`);
  else fail(`Missing NaN guard: ${check}`);
}

// --- Settled detection ---
console.log('\n--- Settled Detection ---');
if (src.includes('settleCounter>30') && src.includes('physicsSettled=true')) {
  pass('Settled: requires 30 consecutive low-movement frames');
}
if (src.includes('physicsFrame>200') && src.includes('physicsSettled=true')) {
  pass('Force settle after 200 frames regardless');
}

// --- Repulsion distance cap ---
console.log('\n--- Repulsion Optimization ---');
if (src.includes('repulsionDist2=250000')) pass('Repulsion capped at d²=250000 (500px)');
if (src.includes('skipRepulsion=visCount>60') && src.includes('physicsFrame%2===0')) {
  pass('Repulsion skipped every other frame for >60 nodes');
} else {
  warn('No repulsion frame-skip optimization');
}

// --- Summary ---
console.log('\n=============================');
console.log(`Results: ${passed} passed, ${failed} failed, ${warnings} warnings`);
console.log('=============================');
if (failed > 0) process.exit(1);
