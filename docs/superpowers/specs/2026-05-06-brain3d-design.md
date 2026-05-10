# MindBank 3D Brain Visualization — Design Spec

> **Date:** 2026-05-06
> **Status:** Approved
> **Approach:** Custom Rust WASM physics + Three.js rendering

---

## Goal

Add an immersive 3D force-directed graph visualization to MindBank that feels like "traveling through a mind" — with glowing nodes, flowing edges, namespace clusters, and an info panel for inspecting nodes/edges. Two entry points: a standalone page and a dashboard tab. Go backend requires zero changes.

---

## Architecture

```
┌─────────────────────────────────────────────────────────────┐
│  BROWSER                                                    │
│  ┌─────────────┐  ┌─────────────┐  ┌─────────────────────┐  │
│  │ Dashboard   │  │ Standalone  │  │ 3D Brain Tab        │  │
│  │ (index.html)│  │ (/brain-3d) │  │ (embedded in dash)  │  │
│  └──────┬──────┘  └──────┬──────┘  └──────────┬──────────┘  │
│         │                │                    │             │
│         └────────────────┴────────────────────┘             │
│                          │                                  │
│              ┌───────────▼────────────┐                    │
│              │  JS Bridge (brain3d.js)  │                    │
│              │  - Fetch /api/v1/graph   │                    │
│              │  - Pass to WASM          │                    │
│              │  - Receive positions     │                    │
│              │  - Drive Three.js        │                    │
│              └───────────┬────────────┘                    │
│                          │                                  │
│              ┌───────────▼────────────┐                    │
│              │  WASM (brain3d.wasm)   │                    │
│              │  - Force-directed      │                    │
│              │  - Namespace gravity   │                    │
│              │  - Edge-type weights   │                    │
│              └────────────────────────┘                    │
│                          │                                  │
│              ┌───────────▼────────────┐                    │
│              │  Three.js Renderer     │                    │
│              │  - Nodes as spheres    │                    │
│              │  - Edges as tubes      │                    │
│              │  - Labels as sprites   │                    │
│              └────────────────────────┘                    │
└─────────────────────────────────────────────────────────────┘
                              │
                              ▼
┌─────────────────────────────────────────────────────────────┐
│  GO BACKEND (unchanged)                                     │
│  - /api/v1/graph → JSON {nodes, edges}                     │
│  - /api/v1/graph?namespace=X → filtered                   │
│  - Same format as 2D graph                                  │
└─────────────────────────────────────────────────────────────┘
```

**Key principle:** Go backend serves identical JSON. No API changes. All 3D logic is frontend.

---

## Component Breakdown

### 1. Rust WASM Physics Engine (`brain3d-physics/`)

```
brain3d-physics/
├── Cargo.toml
├── src/
│   ├── lib.rs          — WASM entry points, JS bridge via wasm-bindgen
│   ├── force.rs        — Force-directed physics (spring, repulsion, gravity)
│   ├── layout.rs       — Namespace clustering, edge-type weights
│   └── types.rs        — Node/Edge data structures (mirror Go API)
```

**Physics forces:**

| Force | Description | Parameters |
|-------|-------------|------------|
| Spring | Edges pull connected nodes together | Strength varies by edge type |
| Repulsion | All nodes push apart (inverse-square) | Cutoff at max distance |
| Namespace gravity | Nodes in same namespace attracted to cluster center | Cluster radius configurable |
| Center gravity | All nodes pulled to origin | Prevents drift |
| Importance mass | Higher importance = larger node = stronger repulsion | Mass = 1.0 + importance |

**WASM API (exposed to JS):**

```rust
#[wasm_bindgen]
pub struct PhysicsEngine;

#[wasm_bindgen]
impl PhysicsEngine {
    pub fn new() -> Self;
    pub fn load_graph(&mut self, nodes: JsValue, edges: JsValue);
    pub fn step(&mut self, dt: f32);
    pub fn get_positions(&self) -> Float32Array;  // [x,y,z, x,y,z, ...]
    pub fn set_namespace_strength(&mut self, strength: f32);
    pub fn set_edge_type_weight(&mut self, edge_type: String, weight: f32);
    pub fn stabilize(&mut self, max_steps: u32) -> bool;  // true if settled
}
```

### 2. JavaScript Bridge (`internal/handler/static/brain3d.js`)

**Responsibilities:**
1. Fetch `/api/v1/graph` (with optional `?namespace=` filter)
2. Serialize nodes/edges to WASM-compatible format
3. Instantiate WASM physics engine
4. Run physics steps in animation loop
5. Read positions, update Three.js scene
6. Handle user interaction (hover, click, camera)
7. Manage info panel state

**Data serialization to WASM:**

```javascript
// Flat arrays for efficient WASM transfer
const nodeIds = nodes.map(n => n.id);
const nodePositions = new Float32Array(nodes.length * 3);  // x,y,z per node
const nodeTypes = new Uint8Array(nodes.map(n => typeToIndex(n.node_type)));
const nodeImportance = new Float32Array(nodes.map(n => n.importance || 0.5));
const nodeNamespace = new Uint32Array(nodes.map(n => nsToIndex(n.namespace)));
const edgeSources = new Uint32Array(edges.map(e => idToIndex(e.source)));
const edgeTargets = new Uint32Array(edges.map(e => idToIndex(e.target)));
const edgeTypes = new Uint8Array(edges.map(e => edgeTypeToIndex(e.edge_type)));
const edgeWeights = new Float32Array(edges.map(e => e.weight || 1.0));
```

### 3. Three.js Renderer

**Scene composition:**

| Element | Three.js Object | Visual |
|---------|-----------------|--------|
| Nodes | InstancedMesh (SphereGeometry) | Glowing spheres, color by type, size by importance |
| Edges | TubeGeometry along CatmullRomCurve3 | Curved tubes, color by type, opacity by weight |
| Node glow | Sprite (glow texture) | Bloom post-processing |
| Edge flow | Points + custom shader | Animated particles traveling along edges |
| Labels | Sprite (canvas texture) | Text labels, fade with distance |
| Namespace halos | SphereGeometry (transparent) | Subtle region indicators |
| Background | Scene fog + particles | Deep space nebula feel |

**Post-processing:**
- UnrealBloomPass for node glow
- FXAA for anti-aliasing
- Tone mapping (ACESFilmic)

### 4. Info Panel (Right Slide-out)

**Layout:**

```
┌─────────────────────────┐
│  [decision] Auth choice │ ← Type badge + Label
│  mindbank namespace     │ ← Namespace chip
│                         │
│  Content:               │
│  ┌───────────────────┐  │
│  │ We chose JWT...   │  │ ← Scrollable <div>
│  │ over sessions...  │  │
│  └───────────────────┘  │
│                         │
│  Importance: 0.9  ★★★★ │
│  Accessed: 42 times     │
│                         │
│  Connected edges (12):  │
│  ┌───────────────────┐  │
│  │ → depends_on API  │  │ ← Scrollable list
│  │ ← supports Auth   │  │
│  └───────────────────┘  │
│                         │
│  [Focus] [Neighbors]    │ ← Action buttons
└─────────────────────────┘
```

**Panel behavior:**
- Slides in from right (300px width)
- Closes with X button or Escape key
- Content scrolls if exceeds panel height
- Edge list items are clickable (navigate to connected node)

---

## Visual Design — "Traveling Through a Mind"

### Color Palette

| Element | Color | Hex |
|---------|-------|-----|
| Background | Deep space black | #050608 |
| Fog | Subtle nebula | #0a0a12 |
| Decision nodes | Cyan | #00d4aa |
| Fact nodes | Blue | #3b82f6 |
| Problem nodes | Red | #ef4444 |
| Preference nodes | Yellow | #f5c518 |
| Project nodes | Green | #39ff14 |
| Person nodes | Magenta | #ec4899 |
| Other nodes | Gray | #6b7b8f |
| depends_on edges | Red | #ef4444 |
| supports edges | Green | #39ff14 |
| relates_to edges | Gray | #6b7b8f |
| learned_from edges | Blue | #3b82f6 |
| Edge flow particles | White | #ffffff |

### Node Appearance
- Base size: 0.3 units + (importance * 1.2)
- Glow radius: 2x node size
- Pulse animation for nodes with access_count > 10 (recently active)
- Selected node: white ring + label always visible

### Edge Appearance
- Width: 0.02 + (weight * 0.05)
- Opacity: 0.3 + (weight * 0.5)
- Curvature: slight arc (CatmullRom with control point offset)
- Flow particles: small spheres moving along edge at speed proportional to weight

### Camera & Controls

| Input | Action |
|-------|--------|
| Left drag | Orbit around focus point |
| Right drag | Pan |
| Scroll | Zoom (dolly) |
| Double-click node | Focus camera on node + highlight neighbors |
| Space | Toggle fly mode (WASD + mouse look) |
| R | Reset camera to origin |
| F | Focus on selected node |
| Escape | Close info panel |

**Camera modes:**
- Orbit mode: rotates around a focus point (default)
- Fly mode: free 3D movement like a drone (WASD + mouse)
- Smooth transitions between modes and focus targets

---

## Interaction Design

### Hover
- Node: scale up 1.2x, show tooltip (label + type)
- Edge: brighten, show tooltip (type + weight)

### Click Node
1. Open info panel with full node details
2. Center camera on node (smooth transition)
3. Highlight node + neighbors (dim others)
4. Show connected edges with full opacity

### Click Edge
1. Open info panel with edge details
2. Highlight edge + source/target nodes
3. Show edge type, weight, and connected nodes

### Double-click Node
1. Same as click
2. Plus: expand 1-hop neighbors (if hidden)

---

## Integration Points

### Dashboard Tab (`index.html`)
- New tab "Brain 3D" in nav bar
- Embeds `brain3d.html` via iframe or direct inclusion
- Inherits namespace filter from dashboard
- Panel width: 100% of tab content area

### Standalone Page (`/brain-3d`)
- Full-screen experience
- Same code as dashboard tab, just different container
- URL: `/brain-3d?namespace=mindbank` for direct links
- Back button returns to dashboard

### Go Router Changes

```go
// In internal/handler/router.go, add:
r.Get("/brain-3d", func(w http.ResponseWriter, r *http.Request) {
    data, _ := staticFS.ReadFile("static/brain3d.html")
    w.Header().Set("Content-Type", "text/html")
    w.Write(data)
})
```

**No API endpoint changes.** `/api/v1/graph` already supports namespace filtering.

---

## File Structure

```
mindbank/
├── brain3d-physics/              ← NEW Rust crate
│   ├── Cargo.toml
│   └── src/
│       ├── lib.rs
│       ├── force.rs
│       ├── layout.rs
│       └── types.rs
├── internal/
│   └── handler/
│       └── static/
│           ├── brain3d.html      ← NEW 3D page shell
│           ├── brain3d.js        ← NEW JS bridge + Three.js setup
│           ├── brain3d.css       ← NEW styles for panel + page
│           └── pkg/              ← NEW wasm-pack output
│               ├── brain3d_physics.js
│               └── brain3d_physics_bg.wasm
└── scripts/
    └── build-brain3d.sh        ← NEW build script
```

---

## Build Process

```bash
# 1. Build WASM
$ cd brain3d-physics
$ wasm-pack build --target web --out-dir ../internal/handler/static/pkg/

# 2. No Go rebuild needed (static files served from embedded FS)
# 3. Restart mindbank-api to pick up new static files
```

**Build script:** `scripts/build-brain3d.sh`

```bash
#!/bin/bash
set -e
cd "$(dirname "$0")/../brain3d-physics"
wasm-pack build --target web --out-dir ../internal/handler/static/pkg/
echo "Brain3D WASM built successfully"
```

---

## Performance Targets

| Metric | Target | Notes |
|--------|--------|-------|
| Initial load | < 3s | WASM + Three.js + graph data |
| Physics settle | < 5s | For 500 nodes |
| FPS | 60 | With 500 nodes, 1000 edges |
| Max nodes | 2000 | Before FPS drops below 30 |
| Memory | < 100MB | Total JS + WASM heap |

**Optimization strategies:**
- InstancedMesh for nodes (1 draw call)
- Merge geometries for edges by type
- LOD: hide labels beyond certain distance
- Physics: adaptive step size (larger steps when settled)
- WASM: use Float32Array views (zero-copy)

---

## Testing Strategy

### Unit Tests (Rust)
- Force calculation correctness
- Energy conservation (physics should not explode)
- Namespace clustering behavior
- Edge-type weight application

### Integration Tests (JS + WASM)
- Graph load → positions non-NaN
- Physics settle → positions stable
- Click node → info panel opens with correct data
- Namespace filter → only filtered nodes visible

### Manual QA
- 500 nodes: smooth interaction
- 1000 nodes: acceptable performance
- Mobile: touch controls work

---

## Mutables (Assumption Tracking)

| Mutable | Status | Resolution |
|---------|--------|------------|
| wasm-bindgen available | KNOWN | cargo install confirmed |
| Three.js CDN reliable | KNOWN | unpkg.com or cdnjs |
| InstancedMesh performance | UNKNOWN | Test with 1000 nodes |
| Mobile GPU performance | UNKNOWN | Deferred — desktop first |
| Edge curve rendering cost | UNKNOWN | Test with 1000 edges |

---

## Deferred Features (Post-MVP)

1. **Time-based layout** — animate graph evolution over time
2. **Search highlight** — find node, fly camera to it
3. **Minimap** — 2D overview in corner
4. **VR mode** — WebXR for immersive exploration
5. **Screenshot/export** — save current view as PNG
6. **Custom shaders** — per-node-type visual effects

---

## Spec Self-Review

- [x] **Placeholder scan:** No TBDs, TODOs, or incomplete sections
- [x] **Internal consistency:** Architecture matches component breakdown, data flow matches API
- [x] **Scope check:** MVP is focused — WASM physics + Three.js + info panel + 2 integration points
- [x] **Ambiguity check:** All requirements explicit (colors, forces, interactions, file paths)

---

*Spec written. Ready for user review before implementation planning.*
