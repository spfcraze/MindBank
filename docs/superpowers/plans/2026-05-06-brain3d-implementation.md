# Brain3D Implementation Plan

> **For Hermes:** Use subagent-driven-development skill to implement this plan task-by-task.

**Goal:** Build a 3D force-directed graph visualization for MindBank using custom Rust WASM physics + Three.js rendering, with both standalone page and dashboard tab integration.

**Architecture:** Rust physics engine compiled to WASM handles force-directed layout. JS bridge fetches graph data from Go API, passes it to WASM, reads positions back, and drives Three.js rendering. Two entry points: standalone `/brain-3d` page and dashboard "Brain 3D" tab.

**Tech Stack:** Rust (wasm-bindgen), Three.js (r128+), Go (static file serving only), vanilla JS, CSS.

---

## Task 1: Create Rust crate `brain3d-physics`

**Objective:** Initialize the Rust WASM crate with correct dependencies.

**Files:**
- Create: `brain3d-physics/Cargo.toml`
- Create: `brain3d-physics/src/lib.rs` (stub)
- Create: `brain3d-physics/src/types.rs`
- Create: `brain3d-physics/src/force.rs` (stub)
- Create: `brain3d-physics/src/layout.rs` (stub)

**Step 1: Write Cargo.toml**

```toml
[package]
name = "brain3d-physics"
version = "0.1.0"
edition = "2021"

[lib]
crate-type = ["cdylib", "rlib"]

[dependencies]
wasm-bindgen = "0.2.87"
js-sys = "0.3.64"

[dependencies.web-sys]
version = "0.3.64"
features = ["console"]

[profile.release]
opt-level = 3
lto = true
```

**Step 2: Write types.rs**

```rust
#[derive(Clone, Copy, Debug)]
pub struct Node {
    pub id: u32,
    pub pos: [f32; 3],
    pub vel: [f32; 3],
    pub mass: f32,
    pub node_type: u8,
    pub namespace: u32,
    pub importance: f32,
}

#[derive(Clone, Copy, Debug)]
pub struct Edge {
    pub source: u32,
    pub target: u32,
    pub edge_type: u8,
    pub weight: f32,
}

pub const NODE_TYPE_DECISION: u8 = 0;
pub const NODE_TYPE_FACT: u8 = 1;
pub const NODE_TYPE_PROBLEM: u8 = 2;
pub const NODE_TYPE_PREFERENCE: u8 = 3;
pub const NODE_TYPE_PROJECT: u8 = 4;
pub const NODE_TYPE_PERSON: u8 = 5;
pub const NODE_TYPE_OTHER: u8 = 6;

pub const EDGE_TYPE_DEPENDS_ON: u8 = 0;
pub const EDGE_TYPE_SUPPORTS: u8 = 1;
pub const EDGE_TYPE_RELATES_TO: u8 = 2;
pub const EDGE_TYPE_LEARNED_FROM: u8 = 3;
```

**Step 3: Write force.rs stub**

```rust
use crate::types::{Node, Edge};

pub fn apply_forces(nodes: &mut [Node], edges: &[Edge], dt: f32) {
    // TODO: implement in Task 3
}
```

**Step 4: Write layout.rs stub**

```rust
use crate::types::Node;

pub fn init_positions(nodes: &mut [Node]) {
    // TODO: implement in Task 2
}
```

**Step 5: Write lib.rs stub**

```rust
mod types;
mod force;
mod layout;

use wasm_bindgen::prelude::*;

#[wasm_bindgen]
pub struct PhysicsEngine {
    nodes: Vec<types::Node>,
    edges: Vec<types::Edge>,
}

#[wasm_bindgen]
impl PhysicsEngine {
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        Self { nodes: vec![], edges: vec![] }
    }
}
```

**Step 6: Verify build**

Run: `cd brain3d-physics && cargo check`
Expected: Compiles without errors

**Step 7: Commit**

```bash
git add brain3d-physics/
git commit -m "feat(brain3d): init Rust WASM physics crate"
```

---

## Task 2: Implement position initialization

**Objective:** Randomize initial node positions in a sphere to avoid overlap.

**Files:**
- Modify: `brain3d-physics/src/layout.rs`

**Step 1: Write layout.rs**

```rust
use crate::types::Node;

pub fn init_positions(nodes: &mut [Node]) {
    let n = nodes.len() as f32;
    let radius = (n * 2.0).sqrt(); // spread out based on count
    
    for (i, node) in nodes.iter_mut().enumerate() {
        let t = i as f32 / n.max(1.0);
        let angle = t * 2.0 * std::f32::consts::PI * 17.0; // golden angle approx
        let y = 1.0 - (t * 2.0); // y from 1 to -1
        let r = (1.0 - y * y).sqrt() * radius;
        
        node.pos[0] = r * angle.cos();
        node.pos[1] = y * radius;
        node.pos[2] = r * angle.sin();
        node.vel = [0.0, 0.0, 0.0];
    }
}
```

**Step 2: Add test**

Create: `brain3d-physics/src/layout.rs` (add at bottom, cfg test)

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::Node;

    #[test]
    fn test_init_positions_non_nan() {
        let mut nodes = vec![
            Node { id: 0, pos: [0.0; 3], vel: [0.0; 3], mass: 1.0, node_type: 0, namespace: 0, importance: 0.5 },
            Node { id: 1, pos: [0.0; 3], vel: [0.0; 3], mass: 1.0, node_type: 0, namespace: 0, importance: 0.5 },
        ];
        init_positions(&mut nodes);
        for node in &nodes {
            assert!(!node.pos[0].is_nan());
            assert!(!node.pos[1].is_nan());
            assert!(!node.pos[2].is_nan());
        }
    }
}
```

**Step 3: Run test**

Run: `cd brain3d-physics && cargo test`
Expected: test passes

**Step 4: Commit**

```bash
git add brain3d-physics/src/layout.rs
git commit -m "feat(brain3d): position initialization with golden spiral"
```

---

## Task 3: Implement force-directed physics

**Objective:** Apply 5 forces: spring (edges), repulsion (all pairs), namespace gravity, center gravity, importance mass.

**Files:**
- Modify: `brain3d-physics/src/force.rs`

**Step 1: Write force.rs**

```rust
use crate::types::{Node, Edge};

const SPRING_K: f32 = 0.03;
const REPULSION_K: f32 = 500.0;
const CENTER_GRAVITY: f32 = 0.001;
const NAMESPACE_GRAVITY: f32 = 0.005;
const DAMPING: f32 = 0.9;
const MAX_FORCE: f32 = 10.0;

pub fn apply_forces(nodes: &mut [Node], edges: &[Edge], dt: f32) {
    let n = nodes.len();
    if n == 0 { return; }

    // Compute namespace centers
    let mut ns_centers: std::collections::HashMap<u32, [f32; 3]> = std::collections::HashMap::new();
    let mut ns_counts: std::collections::HashMap<u32, u32> = std::collections::HashMap::new();
    
    for node in nodes.iter() {
        let entry = ns_centers.entry(node.namespace).or_insert([0.0; 3]);
        entry[0] += node.pos[0];
        entry[1] += node.pos[1];
        entry[2] += node.pos[2];
        *ns_counts.entry(node.namespace).or_insert(0) += 1;
    }
    
    for (ns, center) in ns_centers.iter_mut() {
        let count = *ns_counts.get(ns).unwrap_or(&1) as f32;
        center[0] /= count;
        center[1] /= count;
        center[2] /= count;
    }

    let mut forces = vec![[0.0f32; 3]; n];

    // Spring forces (edges)
    for edge in edges {
        let si = edge.source as usize;
        let ti = edge.target as usize;
        if si >= n || ti >= n { continue; }
        
        let dx = nodes[ti].pos[0] - nodes[si].pos[0];
        let dy = nodes[ti].pos[1] - nodes[si].pos[1];
        let dz = nodes[ti].pos[2] - nodes[si].pos[2];
        let dist = (dx*dx + dy*dy + dz*dz).sqrt().max(0.1);
        
        let target_len = 2.0 / edge.weight.max(0.1);
        let f = SPRING_K * (dist - target_len) * edge.weight;
        
        let fx = f * dx / dist;
        let fy = f * dy / dist;
        let fz = f * dz / dist;
        
        forces[si][0] += fx;
        forces[si][1] += fy;
        forces[si][2] += fz;
        forces[ti][0] -= fx;
        forces[ti][1] -= fy;
        forces[ti][2] -= fz;
    }

    // Repulsion (all pairs)
    for i in 0..n {
        for j in (i+1)..n {
            let dx = nodes[j].pos[0] - nodes[i].pos[0];
            let dy = nodes[j].pos[1] - nodes[i].pos[1];
            let dz = nodes[j].pos[2] - nodes[i].pos[2];
            let dist_sq = dx*dx + dy*dy + dz*dz;
            let dist = dist_sq.sqrt().max(0.5);
            
            let mass_i = nodes[i].mass;
            let mass_j = nodes[j].mass;
            let f = REPULSION_K * mass_i * mass_j / (dist_sq * dist);
            let f = f.min(MAX_FORCE);
            
            let fx = f * dx / dist;
            let fy = f * dy / dist;
            let fz = f * dz / dist;
            
            forces[i][0] -= fx;
            forces[i][1] -= fy;
            forces[i][2] -= fz;
            forces[j][0] += fx;
            forces[j][1] += fy;
            forces[j][2] += fz;
        }
    }

    // Center gravity
    for i in 0..n {
        forces[i][0] -= CENTER_GRAVITY * nodes[i].pos[0] * nodes[i].mass;
        forces[i][1] -= CENTER_GRAVITY * nodes[i].pos[1] * nodes[i].mass;
        forces[i][2] -= CENTER_GRAVITY * nodes[i].pos[2] * nodes[i].mass;
    }

    // Namespace gravity
    for i in 0..n {
        if let Some(center) = ns_centers.get(&nodes[i].namespace) {
            let dx = center[0] - nodes[i].pos[0];
            let dy = center[1] - nodes[i].pos[1];
            let dz = center[2] - nodes[i].pos[2];
            forces[i][0] += NAMESPACE_GRAVITY * dx;
            forces[i][1] += NAMESPACE_GRAVITY * dy;
            forces[i][2] += NAMESPACE_GRAVITY * dz;
        }
    }

    // Apply forces
    for i in 0..n {
        nodes[i].vel[0] = (nodes[i].vel[0] + forces[i][0] * dt) * DAMPING;
        nodes[i].vel[1] = (nodes[i].vel[1] + forces[i][1] * dt) * DAMPING;
        nodes[i].vel[2] = (nodes[i].vel[2] + forces[i][2] * dt) * DAMPING;
        
        nodes[i].pos[0] += nodes[i].vel[0] * dt;
        nodes[i].pos[1] += nodes[i].vel[1] * dt;
        nodes[i].pos[2] += nodes[i].vel[2] * dt;
    }
}
```

**Step 2: Add test**

```rust
#[cfg(test)]
mod tests {
    use super::*;
    use crate::types::Node;

    #[test]
    fn test_apply_forces_no_nan() {
        let mut nodes = vec![
            Node { id: 0, pos: [0.0, 0.0, 0.0], vel: [0.0; 3], mass: 1.0, node_type: 0, namespace: 0, importance: 0.5 },
            Node { id: 1, pos: [5.0, 0.0, 0.0], vel: [0.0; 3], mass: 1.0, node_type: 0, namespace: 0, importance: 0.5 },
        ];
        let edges = vec![Edge { source: 0, target: 1, edge_type: 0, weight: 1.0 }];
        
        apply_forces(&mut nodes, &edges, 0.1);
        
        for node in &nodes {
            assert!(!node.pos[0].is_nan());
            assert!(!node.pos[1].is_nan());
            assert!(!node.pos[2].is_nan());
        }
    }
}
```

**Step 3: Run tests**

Run: `cd brain3d-physics && cargo test`
Expected: 2 tests pass

**Step 4: Commit**

```bash
git add brain3d-physics/src/force.rs
git commit -m "feat(brain3d): implement 5-force physics engine"
```

---

## Task 4: Complete WASM bridge in lib.rs

**Objective:** Expose all functions to JavaScript via wasm-bindgen.

**Files:**
- Modify: `brain3d-physics/src/lib.rs`

**Step 1: Write complete lib.rs**

```rust
mod types;
mod force;
mod layout;

use wasm_bindgen::prelude::*;
use js_sys::Float32Array;

#[wasm_bindgen]
pub struct PhysicsEngine {
    nodes: Vec<types::Node>,
    edges: Vec<types::Edge>,
    namespace_strength: f32,
    edge_type_weights: [f32; 4],
}

#[wasm_bindgen]
impl PhysicsEngine {
    #[wasm_bindgen(constructor)]
    pub fn new() -> Self {
        Self {
            nodes: vec![],
            edges: vec![],
            namespace_strength: 1.0,
            edge_type_weights: [1.0, 1.0, 1.0, 1.0],
        }
    }

    pub fn load_graph(
        &mut self,
        node_count: u32,
        node_types: &[u8],
        node_namespaces: &[u32],
        node_importance: &[f32],
        edge_sources: &[u32],
        edge_targets: &[u32],
        edge_types: &[u8],
        edge_weights: &[f32],
    ) {
        self.nodes.clear();
        self.edges.clear();

        for i in 0..node_count as usize {
            self.nodes.push(types::Node {
                id: i as u32,
                pos: [0.0; 3],
                vel: [0.0; 3],
                mass: 1.0 + node_importance.get(i).copied().unwrap_or(0.5),
                node_type: node_types.get(i).copied().unwrap_or(6),
                namespace: node_namespaces.get(i).copied().unwrap_or(0),
                importance: node_importance.get(i).copied().unwrap_or(0.5),
            });
        }

        layout::init_positions(&mut self.nodes);

        let edge_count = edge_sources.len().min(edge_targets.len()).min(edge_types.len()).min(edge_weights.len());
        for i in 0..edge_count {
            self.edges.push(types::Edge {
                source: edge_sources[i],
                target: edge_targets[i],
                edge_type: edge_types[i],
                weight: edge_weights[i] * self.edge_type_weights[edge_types[i] as usize % 4],
            });
        }
    }

    pub fn step(&mut self, dt: f32) {
        force::apply_forces(&mut self.nodes, &self.edges, dt);
    }

    pub fn get_positions(&self) -> Float32Array {
        let arr = Float32Array::new_with_length(self.nodes.len() as u32 * 3);
        for (i, node) in self.nodes.iter().enumerate() {
            arr.set_index(i as u32 * 3, node.pos[0]);
            arr.set_index(i as u32 * 3 + 1, node.pos[1]);
            arr.set_index(i as u32 * 3 + 2, node.pos[2]);
        }
        arr
    }

    pub fn set_namespace_strength(&mut self, strength: f32) {
        self.namespace_strength = strength;
    }

    pub fn set_edge_type_weight(&mut self, edge_type: u8, weight: f32) {
        if (edge_type as usize) < self.edge_type_weights.len() {
            self.edge_type_weights[edge_type as usize] = weight;
        }
    }

    pub fn stabilize(&mut self, max_steps: u32) -> bool {
        for _ in 0..max_steps {
            self.step(0.1);
        }
        true
    }

    pub fn node_count(&self) -> u32 {
        self.nodes.len() as u32
    }
}
```

**Step 2: Verify build**

Run: `cd brain3d-physics && cargo check`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add brain3d-physics/src/lib.rs
git commit -m "feat(brain3d): complete WASM bridge with load/step/get_positions"
```

---

## Task 5: Build WASM and verify output

**Objective:** Compile Rust to WASM and verify JS bindings are generated.

**Files:**
- Create: `scripts/build-brain3d.sh`
- Output: `internal/handler/static/pkg/`

**Step 1: Write build script**

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/../brain3d-physics"
wasm-pack build --target web --out-dir ../internal/handler/static/pkg/
echo "Brain3D WASM built successfully"
```

**Step 2: Make executable and run**

```bash
chmod +x scripts/build-brain3d.sh
./scripts/build-brain3d.sh
```

Expected output: `Brain3D WASM built successfully`

**Step 3: Verify output files**

Check these exist:
- `internal/handler/static/pkg/brain3d_physics.js`
- `internal/handler/static/pkg/brain3d_physics_bg.wasm`

**Step 4: Commit**

```bash
git add scripts/build-brain3d.sh internal/handler/static/pkg/
git commit -m "feat(brain3d): add WASM build script and compiled output"
```

---

## Task 6: Create Three.js renderer module

**Objective:** Build the core Three.js scene with nodes, edges, and post-processing.

**Files:**
- Create: `internal/handler/static/brain3d.js`

**Step 1: Write brain3d.js — Scene Setup**

```javascript
// brain3d.js — Three.js renderer for MindBank 3D graph

const NODE_COLORS = {
    0: 0x00d4aa, // decision
    1: 0x3b82f6, // fact
    2: 0xef4444, // problem
    3: 0xf5c518, // preference
    4: 0x39ff14, // project
    5: 0xec4899, // person
    6: 0x6b7b8f, // other
};

const EDGE_COLORS = {
    0: 0xef4444, // depends_on
    1: 0x39ff14, // supports
    2: 0x6b7b8f, // relates_to
    3: 0x3b82f6, // learned_from
};

class Brain3D {
    constructor(container) {
        this.container = container;
        this.scene = null;
        this.camera = null;
        this.renderer = null;
        this.controls = null;
        this.nodeMesh = null;
        this.edgeMeshes = [];
        this.labelSprites = [];
        this.nodes = [];
        this.edges = [];
        this.physics = null;
        this.selectedNode = null;
        this.raycaster = new THREE.Raycaster();
        this.mouse = new THREE.Vector2();
        this.isRunning = false;
        this.animationId = null;
        
        this.init();
    }

    init() {
        // Scene
        this.scene = new THREE.Scene();
        this.scene.background = new THREE.Color(0x050608);
        this.scene.fog = new THREE.FogExp2(0x0a0a12, 0.02);

        // Camera
        const aspect = this.container.clientWidth / this.container.clientHeight;
        this.camera = new THREE.PerspectiveCamera(60, aspect, 0.1, 1000);
        this.camera.position.set(0, 0, 30);

        // Renderer
        this.renderer = new THREE.WebGLRenderer({ antialias: true });
        this.renderer.setSize(this.container.clientWidth, this.container.clientHeight);
        this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
        this.renderer.toneMapping = THREE.ACESFilmicToneMapping;
        this.container.appendChild(this.renderer.domElement);

        // Controls
        this.controls = new THREE.OrbitControls(this.camera, this.renderer.domElement);
        this.controls.enableDamping = true;
        this.controls.dampingFactor = 0.05;

        // Lights
        const ambient = new THREE.AmbientLight(0x404040, 2);
        this.scene.add(ambient);
        
        const point = new THREE.PointLight(0xffffff, 1, 100);
        point.position.set(10, 10, 10);
        this.scene.add(point);

        // Events
        window.addEventListener('resize', () => this.onResize());
        this.renderer.domElement.addEventListener('click', (e) => this.onClick(e));
        this.renderer.domElement.addEventListener('mousemove', (e) => this.onHover(e));

        // Start loop
        this.isRunning = true;
        this.animate();
    }

    onResize() {
        const w = this.container.clientWidth;
        const h = this.container.clientHeight;
        this.camera.aspect = w / h;
        this.camera.updateProjectionMatrix();
        this.renderer.setSize(w, h);
    }

    animate() {
        if (!this.isRunning) return;
        this.animationId = requestAnimationFrame(() => this.animate());
        
        if (this.physics) {
            this.physics.step(0.016);
            const positions = this.physics.get_positions();
            this.updateNodePositions(positions);
        }
        
        this.controls.update();
        this.renderer.render(this.scene, this.camera);
    }

    destroy() {
        this.isRunning = false;
        if (this.animationId) cancelAnimationFrame(this.animationId);
        this.renderer.dispose();
        this.container.removeChild(this.renderer.domElement);
    }
}
```

**Step 2: Add node creation method**

```javascript
    createNodes(nodeData) {
        this.nodes = nodeData;
        const count = nodeData.length;
        
        // Remove old mesh
        if (this.nodeMesh) {
            this.scene.remove(this.nodeMesh);
            this.nodeMesh.dispose();
        }

        const geometry = new THREE.SphereGeometry(1, 16, 16);
        const material = new THREE.MeshStandardMaterial({
            color: 0xffffff,
            emissive: 0x000000,
            emissiveIntensity: 0.5,
            roughness: 0.4,
            metalness: 0.6,
        });

        this.nodeMesh = new THREE.InstancedMesh(geometry, material, count);
        
        const dummy = new THREE.Object3D();
        const color = new THREE.Color();
        
        for (let i = 0; i < count; i++) {
            const node = nodeData[i];
            const size = 0.3 + (node.importance || 0.5) * 1.2;
            dummy.scale.set(size, size, size);
            dummy.updateMatrix();
            this.nodeMesh.setMatrixAt(i, dummy.matrix);
            
            const typeColor = NODE_COLORS[node.node_type] || NODE_COLORS[6];
            color.setHex(typeColor);
            this.nodeMesh.setColorAt(i, color);
        }
        
        this.nodeMesh.instanceMatrix.needsUpdate = true;
        this.nodeMesh.instanceColor.needsUpdate = true;
        this.scene.add(this.nodeMesh);
    }

    updateNodePositions(positions) {
        if (!this.nodeMesh) return;
        
        const dummy = new THREE.Object3D();
        for (let i = 0; i < this.nodes.length; i++) {
            const x = positions[i * 3];
            const y = positions[i * 3 + 1];
            const z = positions[i * 3 + 2];
            
            dummy.position.set(x, y, z);
            const size = 0.3 + (this.nodes[i].importance || 0.5) * 1.2;
            dummy.scale.set(size, size, size);
            dummy.updateMatrix();
            this.nodeMesh.setMatrixAt(i, dummy.matrix);
        }
        this.nodeMesh.instanceMatrix.needsUpdate = true;
    }
```

**Step 3: Add edge creation**

```javascript
    createEdges(edgeData) {
        this.edges = edgeData;
        
        // Remove old edges
        this.edgeMeshes.forEach(m => this.scene.remove(m));
        this.edgeMeshes = [];

        for (const edge of edgeData) {
            const sourceIdx = this.nodes.findIndex(n => n.id === edge.source);
            const targetIdx = this.nodes.findIndex(n => n.id === edge.target);
            if (sourceIdx === -1 || targetIdx === -1) continue;

            const source = this.nodes[sourceIdx];
            const target = this.nodes[targetIdx];
            
            // Create curved path
            const mid = new THREE.Vector3(
                (source.x + target.x) / 2,
                (source.y + target.y) / 2 + 0.5,
                (source.z + target.z) / 2
            );
            
            const curve = new THREE.CatmullRomCurve3([
                new THREE.Vector3(source.x, source.y, source.z),
                mid,
                new THREE.Vector3(target.x, target.y, target.z)
            ]);

            const width = 0.02 + (edge.weight || 1.0) * 0.05;
            const geometry = new THREE.TubeGeometry(curve, 20, width, 8, false);
            const typeColor = EDGE_COLORS[edge.edge_type] || EDGE_COLORS[2];
            const material = new THREE.MeshStandardMaterial({
                color: typeColor,
                transparent: true,
                opacity: 0.3 + (edge.weight || 1.0) * 0.5,
            });
            
            const mesh = new THREE.Mesh(geometry, material);
            this.edgeMeshes.push(mesh);
            this.scene.add(mesh);
        }
    }
```

**Step 4: Add interaction handlers**

```javascript
    onHover(event) {
        const rect = this.renderer.domElement.getBoundingClientRect();
        this.mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
        this.mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
        
        this.raycaster.setFromCamera(this.mouse, this.camera);
        // TODO: raycast against instanced mesh for hover effect
    }

    onClick(event) {
        const rect = this.renderer.domElement.getBoundingClientRect();
        this.mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
        this.mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;
        
        this.raycaster.setFromCamera(this.mouse, this.camera);
        // TODO: raycast, open info panel
    }
```

**Step 5: Commit**

```bash
git add internal/handler/static/brain3d.js
git commit -m "feat(brain3d): Three.js renderer with nodes, edges, controls"
```

---

## Task 7: Create standalone HTML page

**Objective:** Build the standalone `/brain-3d` page that loads WASM and Three.js.

**Files:**
- Create: `internal/handler/static/brain3d.html`

**Step 1: Write brain3d.html**

```html
<!DOCTYPE html>
<html lang="en">
<head>
    <meta charset="UTF-8">
    <meta name="viewport" content="width=device-width, initial-scale=1.0">
    <title>MindBank — Brain 3D</title>
    <link rel="stylesheet" href="brain3d.css">
    <script type="importmap">
    {
        "imports": {
            "three": "https://unpkg.com/three@0.160.0/build/three.module.js",
            "three/addons/": "https://unpkg.com/three@0.160.0/examples/jsm/"
        }
    }
    </script>
</head>
<body>
    <div id="app">
        <header>
            <h1>🧠 MindBank Brain 3D</h1>
            <div class="controls">
                <select id="namespace-filter">
                    <option value="">All namespaces</option>
                </select>
                <button id="reset-cam">Reset Camera</button>
                <button id="toggle-fly">Fly Mode</button>
                <a href="/" class="back">← Dashboard</a>
            </div>
        </header>
        
        <main id="canvas-container"></main>
        
        <aside id="info-panel" class="hidden">
            <button class="close">×</button>
            <div class="content"></div>
        </aside>
        
        <div id="tooltip"></div>
    </div>

    <script type="module">
        import init, { PhysicsEngine } from './pkg/brain3d_physics.js';
        import { Brain3D } from './brain3d.js';

        async function main() {
            await init();
            
            const container = document.getElementById('canvas-container');
            const brain = new Brain3D(container);
            
            // Fetch graph data
            const namespace = new URLSearchParams(location.search).get('namespace') || '';
            const url = namespace ? `/api/v1/graph?namespace=${encodeURIComponent(namespace)}` : '/api/v1/graph';
            
            const res = await fetch(url);
            const data = await res.json();
            
            // Init physics
            const physics = new PhysicsEngine();
            
            // Map node IDs to indices
            const nodeIdToIndex = {};
            data.nodes.forEach((n, i) => nodeIdToIndex[n.id] = i);
            
            physics.load_graph(
                data.nodes.length,
                new Uint8Array(data.nodes.map(n => typeToIndex(n.node_type))),
                new Uint32Array(data.nodes.map(n => nsToIndex(n.namespace))),
                new Float32Array(data.nodes.map(n => n.importance || 0.5)),
                new Uint32Array(data.edges.map(e => nodeIdToIndex[e.source])),
                new Uint32Array(data.edges.map(e => nodeIdToIndex[e.target])),
                new Uint8Array(data.edges.map(e => edgeTypeToIndex(e.edge_type))),
                new Float32Array(data.edges.map(e => e.weight || 1.0))
            );
            
            brain.physics = physics;
            brain.createNodes(data.nodes);
            brain.createEdges(data.edges);
            
            // Pre-stabilize
            physics.stabilize(100);
            
            // Populate namespace filter
            const namespaces = [...new Set(data.nodes.map(n => n.namespace))].sort();
            const filter = document.getElementById('namespace-filter');
            namespaces.forEach(ns => {
                const opt = document.createElement('option');
                opt.value = ns;
                opt.textContent = ns;
                filter.appendChild(opt);
            });
            if (namespace) filter.value = namespace;
            
            filter.addEventListener('change', () => {
                const ns = filter.value;
                location.href = ns ? `/brain-3d?namespace=${encodeURIComponent(ns)}` : '/brain-3d';
            });
            
            document.getElementById('reset-cam').addEventListener('click', () => {
                brain.camera.position.set(0, 0, 30);
                brain.controls.target.set(0, 0, 0);
            });
        }

        function typeToIndex(t) {
            const map = { decision:0, fact:1, problem:2, preference:3, project:4, person:5 };
            return map[t] || 6;
        }
        
        function nsToIndex(ns) {
            // Simple hash for namespace string
            let h = 0;
            for (let i = 0; i < ns.length; i++) h = ((h << 5) - h) + ns.charCodeAt(i);
            return Math.abs(h) % 1000;
        }
        
        function edgeTypeToIndex(t) {
            const map = { depends_on:0, supports:1, relates_to:2, learned_from:3 };
            return map[t] || 2;
        }

        main().catch(console.error);
    </script>
</body>
</html>
```

**Step 2: Commit**

```bash
git add internal/handler/static/brain3d.html
git commit -m "feat(brain3d): standalone 3D page with WASM init"
```

---

## Task 8: Create CSS styles

**Objective:** Style the 3D page with dark theme, info panel, and responsive layout.

**Files:**
- Create: `internal/handler/static/brain3d.css`

**Step 1: Write brain3d.css**

```css
* { margin: 0; padding: 0; box-sizing: border-box; }

body {
    font-family: 'Segoe UI', system-ui, sans-serif;
    background: #050608;
    color: #e0e0e0;
    overflow: hidden;
    height: 100vh;
}

#app {
    display: flex;
    flex-direction: column;
    height: 100vh;
}

header {
    display: flex;
    align-items: center;
    justify-content: space-between;
    padding: 12px 20px;
    background: rgba(10, 10, 18, 0.9);
    border-bottom: 1px solid rgba(255,255,255,0.05);
    z-index: 10;
}

header h1 { font-size: 18px; font-weight: 600; }

.controls {
    display: flex;
    gap: 12px;
    align-items: center;
}

.controls select, .controls button, .controls a {
    padding: 6px 14px;
    border-radius: 6px;
    border: 1px solid rgba(255,255,255,0.1);
    background: rgba(255,255,255,0.05);
    color: #e0e0e0;
    font-size: 13px;
    cursor: pointer;
    text-decoration: none;
}

.controls button:hover, .controls a:hover {
    background: rgba(255,255,255,0.1);
}

main {
    flex: 1;
    position: relative;
    overflow: hidden;
}

#info-panel {
    position: fixed;
    top: 60px;
    right: 0;
    width: 320px;
    height: calc(100vh - 60px);
    background: rgba(10, 10, 18, 0.95);
    border-left: 1px solid rgba(255,255,255,0.05);
    padding: 20px;
    transform: translateX(100%);
    transition: transform 0.3s ease;
    overflow-y: auto;
    z-index: 20;
}

#info-panel.visible { transform: translateX(0); }

#info-panel .close {
    position: absolute;
    top: 10px;
    right: 10px;
    background: none;
    border: none;
    color: #e0e0e0;
    font-size: 24px;
    cursor: pointer;
}

#info-panel .content {
    margin-top: 30px;
}

#info-panel h2 {
    font-size: 16px;
    margin-bottom: 8px;
    color: #00d4aa;
}

#info-panel .badge {
    display: inline-block;
    padding: 2px 8px;
    border-radius: 4px;
    font-size: 11px;
    text-transform: uppercase;
    margin-bottom: 12px;
}

#info-panel .scroll-box {
    max-height: 200px;
    overflow-y: auto;
    padding: 10px;
    background: rgba(255,255,255,0.03);
    border-radius: 6px;
    margin: 10px 0;
}

#tooltip {
    position: fixed;
    pointer-events: none;
    background: rgba(0,0,0,0.8);
    padding: 6px 12px;
    border-radius: 4px;
    font-size: 12px;
    display: none;
    z-index: 100;
}
```

**Step 2: Commit**

```bash
git add internal/handler/static/brain3d.css
git commit -m "feat(brain3d): dark theme CSS with info panel"
```

---

## Task 9: Add Go router entry

**Objective:** Wire the standalone page into the Go HTTP router.

**Files:**
- Modify: `internal/handler/router.go`

**Step 1: Add route**

Find the existing graph-view route and add below it:

```go
r.Get("/brain-3d", func(w http.ResponseWriter, r *http.Request) {
    data, err := staticFS.ReadFile("static/brain3d.html")
    if err != nil {
        http.Error(w, "Not found", http.StatusNotFound)
        return
    }
    w.Header().Set("Content-Type", "text/html")
    w.Write(data)
})
```

**Step 2: Verify build**

Run: `cd /home/rat/mindbank && go build ./cmd/mindbank-api`
Expected: Compiles without errors

**Step 3: Commit**

```bash
git add internal/handler/router.go
git commit -m "feat(brain3d): add /brain-3d route to Go router"
```

---

## Task 10: Add dashboard tab

**Objective:** Add "Brain 3D" tab to the dashboard index.html.

**Files:**
- Modify: `internal/handler/static/index.html`

**Step 1: Find nav tabs and add new tab**

Locate the existing nav tabs (likely around line 50-80) and add:

```html
<li class="nav-item">
    <a class="nav-link" href="#brain3d" data-bs-toggle="tab">Brain 3D</a>
</li>
```

**Step 2: Find tab panes and add new pane**

Locate existing tab-content divs and add:

```html
<div class="tab-pane fade" id="brain3d">
    <iframe src="/brain-3d" style="width:100%;height:calc(100vh - 120px);border:none;"></iframe>
</div>
```

**Step 3: Verify file exists and check context**

Read the relevant section of index.html to ensure correct insertion point.

**Step 4: Commit**

```bash
git add internal/handler/static/index.html
git commit -m "feat(brain3d): add Brain 3D tab to dashboard"
```

---

## Task 11: Implement info panel

**Objective:** Complete the info panel with scrollable content and edge list.

**Files:**
- Modify: `internal/handler/static/brain3d.js`

**Step 1: Add info panel methods to Brain3D class**

```javascript
    showNodeInfo(node) {
        const panel = document.getElementById('info-panel');
        const content = panel.querySelector('.content');
        
        const typeColors = {
            decision: '#00d4aa', fact: '#3b82f6', problem: '#ef4444',
            preference: '#f5c518', project: '#39ff14', person: '#ec4899'
        };
        
        const connected = this.edges.filter(e => e.source === node.id || e.target === node.id);
        
        content.innerHTML = `
            <h2>${this.escapeHtml(node.label || node.id)}</h2>
            <span class="badge" style="background:${typeColors[node.node_type] || '#6b7b8f'}">
                ${node.node_type}
            </span>
            <p>Namespace: ${this.escapeHtml(node.namespace)}</p>
            
            <div class="scroll-box">
                <p>${this.escapeHtml(node.content || node.summary || 'No content')}</p>
            </div>
            
            <p>Importance: ${'★'.repeat(Math.round((node.importance || 0.5) * 5))}</p>
            <p>Accessed: ${node.access_count || 0} times</p>
            
            <h3>Connected edges (${connected.length})</h3>
            <div class="scroll-box">
                ${connected.map(e => {
                    const otherId = e.source === node.id ? e.target : e.source;
                    const other = this.nodes.find(n => n.id === otherId);
                    return `<div class="edge-item" data-id="${otherId}">
                        ${e.source === node.id ? '→' : '←'} ${e.edge_type} 
                        ${other ? other.label : otherId}
                    </div>`;
                }).join('')}
            </div>
            
            <div class="actions">
                <button id="focus-btn">Focus</button>
                <button id="neighbors-btn">Neighbors</button>
            </div>
        `;
        
        panel.classList.remove('hidden');
        panel.classList.add('visible');
        
        document.getElementById('focus-btn').addEventListener('click', () => {
            this.focusNode(node);
        });
        
        document.getElementById('neighbors-btn').addEventListener('click', () => {
            this.highlightNeighbors(node);
        });
        
        // Close handler
        panel.querySelector('.close').addEventListener('click', () => {
            panel.classList.remove('visible');
            panel.classList.add('hidden');
        });
    }

    escapeHtml(text) {
        const div = document.createElement('div');
        div.textContent = text;
        return div.innerHTML;
    }

    focusNode(node) {
        const idx = this.nodes.findIndex(n => n.id === node.id);
        if (idx === -1) return;
        const positions = this.physics.get_positions();
        const x = positions[idx * 3];
        const y = positions[idx * 3 + 1];
        const z = positions[idx * 3 + 2];
        
        // Smooth camera transition
        const target = new THREE.Vector3(x, y, z);
        this.controls.target.copy(target);
        // TODO: animate camera position
    }

    highlightNeighbors(node) {
        const neighborIds = new Set();
        this.edges.forEach(e => {
            if (e.source === node.id) neighborIds.add(e.target);
            if (e.target === node.id) neighborIds.add(e.source);
        });
        
        // Dim non-neighbor nodes
        const color = new THREE.Color();
        for (let i = 0; i < this.nodes.length; i++) {
            if (neighborIds.has(this.nodes[i].id) || this.nodes[i].id === node.id) {
                const typeColor = NODE_COLORS[this.nodes[i].node_type] || NODE_COLORS[6];
                color.setHex(typeColor);
            } else {
                color.setHex(0x333333);
            }
            this.nodeMesh.setColorAt(i, color);
        }
        this.nodeMesh.instanceColor.needsUpdate = true;
    }
```

**Step 2: Commit**

```bash
git add internal/handler/static/brain3d.js
git commit -m "feat(brain3d): info panel with scrollable content and neighbors"
```

---

## Task 12: Add raycasting for node selection

**Objective:** Enable clicking on nodes to open info panel.

**Files:**
- Modify: `internal/handler/static/brain3d.js`

**Step 1: Add raycast method**

```javascript
    raycastNode() {
        if (!this.nodeMesh) return null;
        
        this.raycaster.setFromCamera(this.mouse, this.camera);
        
        // For InstancedMesh, we need to raycast against the bounding spheres
        const intersection = this.raycaster.intersectObject(this.nodeMesh);
        if (intersection.length > 0) {
            const instanceId = intersection[0].instanceId;
            return this.nodes[instanceId];
        }
        return null;
    }
```

**Step 2: Update onClick**

```javascript
    onClick(event) {
        const node = this.raycastNode();
        if (node) {
            this.selectedNode = node;
            this.showNodeInfo(node);
            this.focusNode(node);
        }
    }
```

**Step 3: Commit**

```bash
git add internal/handler/static/brain3d.js
git commit -m "feat(brain3d): raycasting for node click selection"
```

---

## Task 13: End-to-end test

**Objective:** Verify the full pipeline works: Go API → JS → WASM → Three.js.

**Files:**
- None (manual verification)

**Step 1: Restart API**

```bash
# Kill existing mindbank-api
pkill -f mindbank-api || true

# Rebuild and restart
cd /home/rat/mindbank
go build ./cmd/mindbank-api
./mindbank-api &
```

**Step 2: Open browser**

Navigate to: `http://localhost:8080/brain-3d`

**Step 3: Verify checklist**

- [ ] Page loads without JS errors
- [ ] WASM initializes (check console for "Brain3D WASM built" or similar)
- [ ] Graph data fetched from `/api/v1/graph`
- [ ] Nodes visible as colored spheres
- [ ] Edges visible as tubes
- [ ] Physics running (nodes moving then settling)
- [ ] Click node opens info panel
- [ ] Info panel shows correct data
- [ ] Focus button centers camera on node
- [ ] Neighbors button highlights connections
- [ ] Namespace filter works
- [ ] Dashboard tab shows embedded 3D view

**Step 4: Fix any issues**

Iterate on brain3d.js, brain3d.html, or Rust code as needed.

**Step 5: Final commit**

```bash
git add -A
git commit -m "feat(brain3d): end-to-end integration complete"
```

---

## Task 14: Performance optimization

**Objective:** Ensure 500 nodes run at 60 FPS.

**Files:**
- Modify: `internal/handler/static/brain3d.js`
- Modify: `brain3d-physics/src/lib.rs`

**Step 1: Add adaptive physics stepping**

In `lib.rs`, add early stabilization detection:

```rust
pub fn is_settled(&self) -> bool {
    let threshold = 0.01;
    self.nodes.iter().all(|n| {
        n.vel[0].abs() < threshold &&
        n.vel[1].abs() < threshold &&
        n.vel[2].abs() < threshold
    })
}
```

**Step 2: Pause physics when settled**

In `brain3d.js`, skip physics steps if settled:

```javascript
    animate() {
        if (!this.isRunning) return;
        this.animationId = requestAnimationFrame(() => this.animate());
        
        if (this.physics && !this.physics.is_settled()) {
            this.physics.step(0.016);
            const positions = this.physics.get_positions();
            this.updateNodePositions(positions);
        }
        
        this.controls.update();
        this.renderer.render(this.scene, this.camera);
    }
```

**Step 3: Add LOD for labels**

Hide labels beyond distance threshold:

```javascript
    updateLabels() {
        const camPos = this.camera.position;
        for (let i = 0; i < this.labelSprites.length; i++) {
            const label = this.labelSprites[i];
            const dist = label.position.distanceTo(camPos);
            label.visible = dist < 20; // only show nearby labels
        }
    }
```

**Step 4: Commit**

```bash
git add brain3d-physics/src/lib.rs internal/handler/static/brain3d.js
git commit -m "perf(brain3d): adaptive physics stepping and label LOD"
```

---

## Summary

| Task | Description | Files Touched |
|------|-------------|---------------|
| 1 | Init Rust crate | `brain3d-physics/Cargo.toml`, `src/*.rs` |
| 2 | Position initialization | `src/layout.rs` |
| 3 | Force-directed physics | `src/force.rs` |
| 4 | WASM bridge | `src/lib.rs` |
| 5 | Build WASM | `scripts/build-brain3d.sh`, `static/pkg/` |
| 6 | Three.js renderer | `static/brain3d.js` |
| 7 | Standalone HTML | `static/brain3d.html` |
| 8 | CSS styles | `static/brain3d.css` |
| 9 | Go router | `internal/handler/router.go` |
| 10 | Dashboard tab | `static/index.html` |
| 11 | Info panel | `static/brain3d.js` |
| 12 | Raycasting | `static/brain3d.js` |
| 13 | E2E test | Manual verification |
| 14 | Performance | `src/lib.rs`, `static/brain3d.js` |

**Total estimated time:** 2-3 hours of focused implementation.

**Ready for execution via subagent-driven-development.**
