// brain3d.js — Three.js renderer for MindBank 3D graph
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';
import { CSS2DRenderer, CSS2DObject } from 'three/addons/renderers/CSS2DRenderer.js';
import { GLYPH_CREATORS, createLODGlyph } from './brain3d-glyphs.js';

// ═══════════════════════════════════════════════════════════════════════════
// HOWARD COSMOGONIC CONSTRAINT ENGINE — Applied to Brain 3D Visualization
// ═══════════════════════════════════════════════════════════════════════════
// Core axiom: All memory visualization reduces to the directed relationship.
// Visual form: Toroidal wave field with 4 interacting forces.
// 
// 4 Forces:
//   1. Nodes (Crystallized Memory) — Gyroscopic vortices (TorusGeometry)
//   2. Edges (Relational Current) — Birkeland tendrils (helical TubeGeometry)
//   3. Field (Ambient Context) — Pressure gradient (custom shader)
//   4. Observer (User Gaze) — Measurement collapse interaction
//
// Source: howard skill — howard-lynchpin-tetra-terryen-aubreyen-1778632804
// ═══════════════════════════════════════════════════════════════════════════

// Reference palette from adversarial-currents.html
const NODE_COLORS = [
    0xff6b35, // decision - warm orange [255, 107, 53]
    0x4ecdc4, // fact - teal [78, 205, 196]
    0xa78bfa, // problem - purple [167, 139, 250]
    0xff6b35, // preference - orange (same as decision)
    0x4ecdc4, // project - teal (same as fact)
    0xa78bfa, // person - purple (same as problem)
    0x808080, // unknown - gray
];

// Topic colors for session nodes
const TOPIC_COLORS = {
    'deployment': 0x3b82f6,   // Blue
    'database': 0x10b981,     // Green
    'api': 0xf59e0b,          // Amber
    'auth': 0xef4444,         // Red
    'frontend': 0x8b5cf6,     // Purple
    'bugfix': 0xf97316,       // Orange
    'refactoring': 0x06b6d4,  // Cyan
    'testing': 0x84cc16,      // Lime
    'config': 0x64748b,       // Slate
    'architecture': 0xd946ef, // Fuchsia
    'general': 0x94a3b8       // Gray
};

// Event type colors for lifecycle events
const EVENT_COLORS = {
    'session_start': 0x22c55e,      // Green
    'user_prompt_submit': 0x3b82f6, // Blue
    'pre_tool_use': 0xf59e0b,       // Amber
    'post_tool_use': 0x10b981,      // Teal
    'stop': 0xef4444                // Red
};



const EDGE_COLORS = [
    0x4ecdc4, // depends_on - teal
    0x4ecdc4, // supports - teal
    0x404040, // relates_to - subtle gray
    0x4ecdc4, // learned_from - teal
    0xa78bfa, // influences - purple
    0xff6b35, // contradicts - orange
    0x808080, // other - gray
];

export class Brain3D {
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
        this.nodeGroups = [];  // Initialize early so raycastNode doesn't crash before data loads
        this._hideGlobalNodes = false;  // Track global namespace visibility toggle
        this.physics = null;
        this.selectedNode = null;
        this.raycaster = new THREE.Raycaster();
        this.mouse = new THREE.Vector2();
        this.isRunning = false;
        this.animationId = null;
        this.hoveredNode = null;

        this.init();
        // Expose for debugging
        window.brain3d = this;
        console.log('[Brain3D] initialized, exposed as window.brain3d');
    }

    init() {
        // Scene — deep space with subtle nebula tint
        this.scene = new THREE.Scene();
        this.scene.background = new THREE.Color(0x0a0a1a);
        this.scene.fog = new THREE.FogExp2(0x0a0a1a, 0.008);

        // Camera — better default for large graphs
        const aspect = this.container.clientWidth / this.container.clientHeight;
        this.camera = new THREE.PerspectiveCamera(60, aspect, 0.1, 1000);
        this.camera.position.set(0, 30, 60);

        // Renderer
        this.renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
        this.renderer.setSize(this.container.clientWidth, this.container.clientHeight);
        this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
        this.renderer.toneMapping = THREE.ACESFilmicToneMapping;
        this.renderer.toneMappingExposure = 1.2;
        this.container.appendChild(this.renderer.domElement);

        // CSS2DRenderer for labels
        this.labelRenderer = new CSS2DRenderer();
        this.labelRenderer.setSize(this.container.clientWidth, this.container.clientHeight);
        this.labelRenderer.domElement.style.position = 'absolute';
        this.labelRenderer.domElement.style.top = '0';
        this.labelRenderer.domElement.style.left = '0';
        this.labelRenderer.domElement.style.width = '100%';
        this.labelRenderer.domElement.style.height = '100%';
        this.labelRenderer.domElement.style.pointerEvents = 'none';
        this.container.appendChild(this.labelRenderer.domElement);
        this.labels = [];

        // Post-processing for neon glow
        if (window.EffectComposer && window.UnrealBloomPass && window.RenderPass) {
            this.composer = new window.EffectComposer(this.renderer);
            this.composer.addPass(new window.RenderPass(this.scene, this.camera));
            
            this.bloomPass = new window.UnrealBloomPass(
                new THREE.Vector2(this.container.clientWidth, this.container.clientHeight),
                1.5,  // strength
                0.4,  // radius
                0.85  // threshold
            );
            this.composer.addPass(this.bloomPass);
            console.log('[Brain3D] Bloom post-processing enabled');
        } else {
            console.log('[Brain3D] Bloom not available — post-processing classes not loaded');
        }

        // Controls
        this.controls = new OrbitControls(this.camera, this.renderer.domElement);
        this.controls.enableDamping = true;
        this.controls.dampingFactor = 0.05;
        this.controls.minDistance = 2;
        this.controls.maxDistance = 50;

        // Lights — minimal, nodes are self-illuminated
        const ambient = new THREE.AmbientLight(0x404040, 1);
        this.scene.add(ambient);

        // Events
        window.addEventListener('resize', () => this.onResize());
        this.renderer.domElement.addEventListener('click', (e) => this.onClick(e));
        this.renderer.domElement.addEventListener('mousemove', (e) => this.onHover(e));

        // Touch support
        this._initTouchSupport();

        // Howard: Measurement collapse animation state
        this.collapseAnimations = new Map();
        this._dissolveProgress = 0;
        this._dissolveTarget = 0;

        // HOWARD: Force-based physics state (Tetra-Terryen — 4 forces)
        this.forcePhysics = null;

        // Edge particle system
        this.particleSystem = null;
        this.particleUniforms = null;

        // Real-time activity tracking
        this.lastAccessCounts = new Map();
        this.pollInterval = null;

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
        if (this.composer) {
            this.composer.setSize(w, h);
        }
        if (this.labelRenderer) {
            this.labelRenderer.setSize(w, h);
        }
    }

    animate() {
        if (!this.isRunning) return;
        this.animationId = requestAnimationFrame(() => this.animate());

        const time = performance.now() * 0.001;

        // Grid layout: skip physics, use fixed grid positions
        // Physics is disabled for powergrid layout

        // HOWARD: Force-based physics simulation step (Tetra-Terryen)
        // Static layout: update positions once, no physics step
        if (this.forcePhysics && this.forcePhysics.positions) {
            const positions = this.forcePhysics.getPositions();
            this.updateNodePositions(positions);
        }

        // Howard: Update node rotations (spin axes)
        if (this.nodeSpinSpeeds && this.nodeRotations) {
            for (let i = 0; i < this.nodes.length; i++) {
                const speed = this.nodeSpinSpeeds[i] || 0.5;
                this.nodeRotations[i * 3] += speed * 0.016;
                this.nodeRotations[i * 3 + 1] += speed * 0.008;
            }
        }

        // Howard: Update dissolve animation
        if (this._dissolveProgress !== this._dissolveTarget) {
            const delta = this._dissolveTarget - this._dissolveProgress;
            this._dissolveProgress += delta * 0.02;
            if (Math.abs(this._dissolveTarget - this._dissolveProgress) < 0.001) {
                this._dissolveProgress = this._dissolveTarget;
            }
        }

        // Update edge particles
        if (this.particleUniforms) {
            this.particleUniforms.uTime.value = time;
        }

        // Update edge particle positions
        if (this.forcePhysics && this.forcePhysics.positions) {
            this._updateEdgeParticles(this.forcePhysics.getPositions(), time);
        }

        this.controls.update();
        
        // Update labels
        this._updateLabels();
        
        // Render with bloom if available
        if (this.composer) {
            this.composer.render();
        } else {
            this.renderer.render(this.scene, this.camera);
        }
        
        // Render labels
        if (this.labelRenderer) {
            this.labelRenderer.render(this.scene, this.camera);
        }
    }

    destroy() {
        this.isRunning = false;
        if (this.animationId) cancelAnimationFrame(this.animationId);
        this.renderer.dispose();
        this.container.removeChild(this.renderer.domElement);
    }

    createNodes(nodeData, projectCount = 0) {
        console.log('[Brain3D] createNodes called with', nodeData.length, 'nodes,', projectCount, 'projects');
        this.nodes = nodeData;
        this.projectCount = projectCount;

        // Howard: Initialize per-node rotation state
        this.nodeRotations = new Float32Array(nodeData.length * 3);
        this.nodeSpinSpeeds = new Float32Array(nodeData.length);
        for (let i = 0; i < nodeData.length; i++) {
            this.nodeRotations[i * 3] = Math.random() * Math.PI;
            this.nodeRotations[i * 3 + 1] = Math.random() * Math.PI;
            this.nodeRotations[i * 3 + 2] = Math.random() * Math.PI;
            const acc = nodeData[i].access_count || 0;
            this.nodeSpinSpeeds[i] = 0.5 + Math.min(acc / 10, 3.0);
        }

        // Remove old node groups
        if (this.nodeGroups) {
            this.nodeGroups.forEach(group => {
                this.scene.remove(group);
                group.traverse(child => {
                    if (child.geometry) child.geometry.dispose();
                    if (child.material) child.material.dispose();
                });
            });
        }
        this.nodeGroups = [];

        const dummy = new THREE.Object3D();
        const color = new THREE.Color();

        // Group nodes by type for InstancedMesh
        const nodesByType = {};
        for (let i = 0; i < nodeData.length; i++) {
            const type = nodeData[i].node_type || 'unknown';
            if (!nodesByType[type]) nodesByType[type] = [];
            nodesByType[type].push({ node: nodeData[i], index: i });
        }

        // Create one InstancedMesh per type with custom glyph geometry
        for (const [type, nodes] of Object.entries(nodesByType)) {
            const group = new THREE.Group();
            group.userData.nodeType = type;
            group.userData.indices = nodes.map(n => n.index);

            const typeIdx = this.typeToIndex(type);
            const typeColor = NODE_COLORS[typeIdx] || NODE_COLORS[6];
            color.setHex(typeColor);

            // Get or create glyph geometry
            const creator = GLYPH_CREATORS[type];
            const glyphGeo = creator ? creator() : createLODGlyph();
            
            // Single material with emissive glow for "neural" look
            const material = new THREE.MeshStandardMaterial({
                color: typeColor,
                emissive: typeColor,
                emissiveIntensity: 0.3,
                roughness: 0.4,
                metalness: 0.6,
                transparent: true,
                opacity: 0.95,
            });

            const count = nodes.length;
            const mesh = new THREE.InstancedMesh(glyphGeo, material, count);
            mesh.userData.layer = 'glyph';
            mesh.castShadow = false;
            mesh.receiveShadow = false;

            // Set initial matrices
            for (let i = 0; i < nodes.length; i++) {
                const { node, index } = nodes[i];
                const isProject = index < this.projectCount;
                
                let baseSize;
                if (isProject) baseSize = 1.2;
                else if (node.node_type === 'session') baseSize = 0.3;
                else baseSize = 0.7;
                
                const tierMult = isProject ? this.tierToMult(node.importance || 0.5) : 0.6;
                const size = baseSize * tierMult;

                // Use topic color for sessions if available
                let nodeColor = typeColor;
                if (!isProject && node.metadata && node.metadata.topic && TOPIC_COLORS[node.metadata.topic]) {
                    nodeColor = TOPIC_COLORS[node.metadata.topic];
                }
                // Use event color for event nodes
                if (node.node_type === 'event' && node.event_type && EVENT_COLORS[node.event_type]) {
                    nodeColor = EVENT_COLORS[node.event_type];
                }
                color.setHex(nodeColor);

                dummy.position.set(0, 0, 0);
                dummy.rotation.set(0, 0, 0);
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                mesh.setMatrixAt(i, dummy.matrix);
                mesh.setColorAt(i, color);
            }

            mesh.instanceMatrix.needsUpdate = true;
            mesh.instanceColor.needsUpdate = true;

            group.add(mesh);
            this.nodeGroups.push(group);
            this.scene.add(group);
        }

        // Store index-to-group mapping
        this.nodeIndexMap = new Array(nodeData.length);
        this.nodeGroups.forEach((group, groupIdx) => {
            group.userData.indices.forEach((globalIdx, localIdx) => {
                this.nodeIndexMap[globalIdx] = { group: groupIdx, local: localIdx };
            });
        });

        // Create labels
        this._createLabels(nodeData);
    }

    _createLabels(nodeData) {
        this._clearLabels();
        
        for (let i = 0; i < nodeData.length; i++) {
            const node = nodeData[i];
            const labelText = (node.label || node.id).substring(0, 20);
            
            const div = document.createElement('div');
            div.className = 'brain3d-label';
            div.textContent = labelText;
            div.style.cssText = `
                color: rgba(255,255,255,0.9);
                font-family: 'Inter', sans-serif;
                font-size: 11px;
                font-weight: 500;
                text-shadow: 0 1px 3px rgba(0,0,0,0.8);
                pointer-events: none;
                white-space: nowrap;
                transition: opacity 0.3s;
                opacity: 0;
            `;
            
            const label = new CSS2DObject(div);
            label.position.set(0, 0, 0);
            label.userData.nodeIndex = i;
            this.scene.add(label);
            this.labels.push(label);
        }
    }

    _clearLabels() {
        for (const label of this.labels) {
            this.scene.remove(label);
            if (label.element && label.element.parentNode) {
                label.element.parentNode.removeChild(label.element);
            }
        }
        this.labels = [];
    }

    _updateLabels() {
        if (!this.labels || this.labels.length === 0) return;
        
        const camPos = this.camera.position;
        
        // In focus mode, show labels for selected + direct neighbors
        const focusNeighborIds = new Set();
        if (this._focusedNode) {
            focusNeighborIds.add(this._focusedNode.id);
            for (const edge of this.edges) {
                if (edge.source === this._focusedNode.id) focusNeighborIds.add(edge.target);
                if (edge.target === this._focusedNode.id) focusNeighborIds.add(edge.source);
            }
        }
        
        for (const label of this.labels) {
            const idx = label.userData.nodeIndex;
            const mapping = this.nodeIndexMap[idx];
            if (!mapping) continue;
            
            const group = this.nodeGroups[mapping.group];
            const mesh = group.children[0];
            const matrix = new THREE.Matrix4();
            mesh.getMatrixAt(mapping.local, matrix);
            const pos = new THREE.Vector3();
            pos.setFromMatrixPosition(matrix);
            
            label.position.copy(pos);
            label.position.y += 0.8;
            
            const node = this.nodes[idx];
            const dist = camPos.distanceTo(pos);
            
            // Determine if label should be visible
            let showLabel = false;
            let opacity = 0;
            
            if (node.node_type === 'project') {
                // Projects are always labeled (if close enough)
                showLabel = true;
                opacity = Math.min(1, Math.max(0, (40 - dist) / 20));
            } else if (this._focusedNode) {
                // In focus mode: show selected + neighbors
                if (focusNeighborIds.has(node.id)) {
                    showLabel = true;
                    opacity = Math.min(1, Math.max(0, (30 - dist) / 15));
                }
            } else if (node.node_type === 'decision' || node.node_type === 'problem') {
                // Decisions/problems labeled when close
                showLabel = dist < 20;
                opacity = Math.min(1, Math.max(0, (20 - dist) / 10));
            }
            
            // Selected node always labeled
            if (this.selectedNode && this.selectedNode.id === node.id) {
                showLabel = true;
                opacity = 1;
            }
            
            label.element.style.opacity = showLabel ? opacity : 0;
        }
    }
    tierToMult(importance) {
        // Map importance (0-1) to tier multiplier like reference
        if (importance >= 0.8) return 1.5;  // Tier 3 - largest
        if (importance >= 0.5) return 1.2;  // Tier 2
        if (importance >= 0.3) return 0.9;  // Tier 1
        return 0.6;  // Tier 0 - smallest
    }

    updateNodePositions(positions) {
        if (!this.nodeGroups || this.nodeGroups.length === 0) return;

        const dummy = new THREE.Object3D();
        const time = performance.now() * 0.001;
        
        // Focus mode: compute neighbor distances if focused
        const focusDistances = this._computeFocusDistances();

        for (let i = 0; i < this.nodes.length; i++) {
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;

            const group = this.nodeGroups[mapping.group];
            const node = this.nodes[i];
            const mesh = group.children[0];

            const isHidden = this._hideGlobalNodes && node.namespace === 'global';

            // Type-based size hierarchy (Understand-Anything style)
            const typeSizeMult = this._getTypeSizeMultiplier(node.node_type);
            const baseSize = 0.3 + (node.importance || 0.5) * 0.4;
            const tierMult = this.tierToMult(node.importance || 0.5);
            let size = baseSize * tierMult * typeSizeMult;

            // Focus mode: scale selected node larger
            if (this._focusedNode && this._focusedNode.id === node.id) {
                size *= 1.5;
            }

            if (isHidden) {
                dummy.position.set(99999, 99999, 99999);
                dummy.rotation.set(0, 0, 0);
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                mesh.setMatrixAt(mapping.local, dummy.matrix);
                continue;
            }

            const x = positions[i * 3];
            const y = positions[i * 3 + 1];
            const z = positions[i * 3 + 2];

            const pulsePhase = i * 0.5;
            const glow = Math.sin(time * 0.03 + pulsePhase) * 0.3 + 0.7;

            dummy.position.set(x, y, z);

            if (this.nodeRotations) {
                dummy.rotation.set(
                    this.nodeRotations[i * 3],
                    this.nodeRotations[i * 3 + 1],
                    this.nodeRotations[i * 3 + 2]
                );
            } else {
                dummy.rotation.set(0, 0, 0);
            }

            let scaleMult = 1.0;
            const collapse = this.collapseAnimations ? this.collapseAnimations.get(i) : null;
            if (collapse) {
                const elapsed = performance.now() - collapse.startTime;
                const progress = Math.min(elapsed / collapse.duration, 1.0);
                const ease = 1 - Math.pow(1 - progress, 3);
                scaleMult = collapse.fromScale + (collapse.toScale - collapse.fromScale) * ease;
                if (progress >= 1.0) {
                    this.collapseAnimations.delete(i);
                }
            }

            // Focus mode opacity
            let focusOpacity = 1.0;
            if (focusDistances) {
                const dist = focusDistances[i];
                if (dist === 0) focusOpacity = 1.0;           // Selected
                else if (dist === 1) focusOpacity = 0.8;      // Direct neighbor
                else if (dist === 2) focusOpacity = 0.4;      // 2-hop
                else focusOpacity = 0.08;                      // Ghosted
            }

            let dissolveScale = 1.0;
            let dissolveOpacity = 1.0;
            if (this._dissolveProgress > 0) {
                const dp = this._dissolveProgress;
                dissolveScale = 1 - dp * 0.5;
                dissolveOpacity = 1 - dp;
            }

            const finalScale = size * glow * scaleMult * dissolveScale;
            dummy.scale.set(finalScale, finalScale, finalScale);
            dummy.updateMatrix();
            mesh.setMatrixAt(mapping.local, dummy.matrix);
            
            // Update material opacity for focus mode
            mesh.material.opacity = 0.95 * dissolveOpacity * focusOpacity;
        }

        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
        });
    }

    // Type-based size multiplier (Understand-Anything hierarchical sizing)
    _getTypeSizeMultiplier(nodeType) {
        switch (nodeType) {
            case 'project': return 2.0;
            case 'decision': return 1.2;
            case 'problem': return 1.0;
            case 'advice': return 0.5;
            case 'preference': return 0.5;
            case 'fact': return 0.35;
            case 'topic': return 0.6;
            case 'concept': return 0.6;
            case 'event': return 0.6;
            case 'agent': return 0.7;
            case 'person': return 0.7;
            case 'question': return 0.6;
            default: return 0.5;
        }
    }

    // Compute focus distances for all nodes (0 = selected, 1 = neighbor, 2 = 2-hop, etc.)
    _computeFocusDistances() {
        if (!this._focusedNode) return null;
        
        const distances = new Array(this.nodes.length).fill(-1);
        const focusIdx = this.nodes.findIndex(n => n.id === this._focusedNode.id);
        if (focusIdx === -1) return null;
        
        distances[focusIdx] = 0;
        
        // BFS to find distances
        const queue = [focusIdx];
        const visited = new Set([focusIdx]);
        
        while (queue.length > 0) {
            const current = queue.shift();
            const currentDist = distances[current];
            
            if (currentDist >= 2) continue; // Only compute up to 2 hops
            
            for (const edge of this.edges) {
                const srcIdx = this.nodeIdToIndex[edge.source];
                const tgtIdx = this.nodeIdToIndex[edge.target];
                
                if (srcIdx === current && !visited.has(tgtIdx)) {
                    visited.add(tgtIdx);
                    distances[tgtIdx] = currentDist + 1;
                    queue.push(tgtIdx);
                } else if (tgtIdx === current && !visited.has(srcIdx)) {
                    visited.add(srcIdx);
                    distances[srcIdx] = currentDist + 1;
                    queue.push(srcIdx);
                }
            }
        }
        
        return distances;
    }
    // ═══════════════════════════════════════════════════════════════════════════
    // HOWARD: Force-based Physics Engine (Tetra-Terryen — 4 interacting forces)
    // Position emerges from relational pressure, not predefined geometry.
    // ═══════════════════════════════════════════════════════════════════════════
    // ═══════════════════════════════════════════════════════════════════════════
    // STATIC SPATIAL LAYOUT — Projects as anchors, namespace clusters
    // Replaces force-based physics with computed fixed positions
    // ═══════════════════════════════════════════════════════════════════════════
    initForcePhysics(nodes, edges, nodeIdToIndex) {
        const count = nodes.length;
        const pos = new Float32Array(count * 3);

        // ─── STEP 1: Separate projects and non-projects ───
        const projects = [];
        const nonProjects = [];
        for (let i = 0; i < count; i++) {
            if (nodes[i].node_type === 'project') {
                projects.push({index: i, node: nodes[i]});
            } else {
                nonProjects.push({index: i, node: nodes[i]});
            }
        }

        // Sort projects by importance (highest first)
        projects.sort((a, b) => (b.node.importance || 0.5) - (a.node.importance || 0.5));

        // ─── STEP 2: Position projects using organic force-directed layout ───
        const projectPositions = new Map();
        
        // Initialize with random positions in a loose sphere
        const PROJECT_SPREAD = 40;
        for (let p = 0; p < projects.length; p++) {
            const i = projects[p].index;
            const seed = p * 137.5;
            const theta = seed * (Math.PI / 180);
            const phi = Math.acos(1 - 2 * ((p + 0.5) / Math.max(projects.length, 1)));
            
            const r = PROJECT_SPREAD * (0.5 + 0.5 * Math.sqrt(p / Math.max(projects.length, 1)));
            const x = r * Math.sin(phi) * Math.cos(theta);
            const z = r * Math.sin(phi) * Math.sin(theta);
            const y = r * Math.cos(phi) * 0.3;
            
            pos[i * 3] = x;
            pos[i * 3 + 1] = y;
            pos[i * 3 + 2] = z;
            projectPositions.set(projects[p].node.id, {x, y, z, index: i});
        }

        // Run force-directed iterations to spread projects naturally
        const projectCount = projects.length;
        for (let iter = 0; iter < 50; iter++) {
            const forces = new Array(projectCount * 3).fill(0);
            
            for (let a = 0; a < projectCount; a++) {
                for (let b = a + 1; b < projectCount; b++) {
                    const ax = pos[projects[a].index * 3];
                    const ay = pos[projects[a].index * 3 + 1];
                    const az = pos[projects[a].index * 3 + 2];
                    const bx = pos[projects[b].index * 3];
                    const by = pos[projects[b].index * 3 + 1];
                    const bz = pos[projects[b].index * 3 + 2];
                    
                    const dx = ax - bx;
                    const dy = ay - by;
                    const dz = az - bz;
                    const dist = Math.sqrt(dx*dx + dy*dy + dz*dz) || 1;
                    
                    const force = 200 / (dist * dist);
                    const fx = (dx / dist) * force;
                    const fy = (dy / dist) * force;
                    const fz = (dz / dist) * force;
                    
                    forces[a * 3] += fx;
                    forces[a * 3 + 1] += fy;
                    forces[a * 3 + 2] += fz;
                    forces[b * 3] -= fx;
                    forces[b * 3 + 1] -= fy;
                    forces[b * 3 + 2] -= fz;
                }
            }
            
            for (let a = 0; a < projectCount; a++) {
                const px = pos[projects[a].index * 3];
                const py = pos[projects[a].index * 3 + 1];
                const pz = pos[projects[a].index * 3 + 2];
                
                forces[a * 3] -= px * 0.001;
                forces[a * 3 + 1] -= py * 0.001;
                forces[a * 3 + 2] -= pz * 0.001;
            }
            
            for (let a = 0; a < projectCount; a++) {
                pos[projects[a].index * 3] += forces[a * 3] * 0.1;
                pos[projects[a].index * 3 + 1] += forces[a * 3 + 1] * 0.1;
                pos[projects[a].index * 3 + 2] += forces[a * 3 + 2] * 0.1;
            }
        }

        for (const proj of projects) {
            const i = proj.index;
            projectPositions.set(proj.node.id, {
                x: pos[i * 3],
                y: pos[i * 3 + 1],
                z: pos[i * 3 + 2],
                index: i
            });
        }

        // ─── STEP 3: Group non-project nodes by namespace ───
        const nsGroups = new Map();
        for (const item of nonProjects) {
            const ns = item.node.namespace || 'global';
            if (!nsGroups.has(ns)) nsGroups.set(ns, []);
            nsGroups.get(ns).push(item);
        }

        // ─── STEP 4: Find parent project for each namespace ───
        const nsToProject = new Map();
        for (const [ns, items] of nsGroups) {
            const matchingProject = projects.find(p => p.node.namespace === ns);
            if (matchingProject) {
                nsToProject.set(ns, matchingProject.node.id);
                continue;
            }

            const projectEdgeCounts = new Map();
            for (const item of items) {
                for (const edge of edges) {
                    if (edge.source === item.node.id || edge.target === item.node.id) {
                        const otherId = edge.source === item.node.id ? edge.target : edge.source;
                        const otherIdx = nodeIdToIndex[otherId];
                        if (otherIdx !== undefined && nodes[otherIdx].node_type === 'project') {
                            projectEdgeCounts.set(otherId, (projectEdgeCounts.get(otherId) || 0) + 1);
                        }
                    }
                }
            }

            if (projectEdgeCounts.size > 0) {
                let bestProject = null;
                let bestCount = -1;
                for (const [pid, cnt] of projectEdgeCounts) {
                    if (cnt > bestCount) {
                        bestCount = cnt;
                        bestProject = pid;
                    }
                }
                nsToProject.set(ns, bestProject);
            } else {
                nsToProject.set(ns, projects[0]?.node.id);
            }
        }

        // ─── STEP 5: Position non-project nodes organically ───
        for (const [ns, items] of nsGroups) {
            const parentProjectId = nsToProject.get(ns);
            const parentProject = projectPositions.get(parentProjectId);

            if (!parentProject) {
                const orphanAngle = Math.random() * Math.PI * 2;
                const orphanRadius = 45 + Math.random() * 15;
                const ox = Math.cos(orphanAngle) * orphanRadius;
                const oz = Math.sin(orphanAngle) * orphanRadius;
                const oy = (Math.random() - 0.5) * 10;
                for (const item of items) {
                    pos[item.index * 3] = ox + (Math.random() - 0.5) * 12;
                    pos[item.index * 3 + 1] = oy + (Math.random() - 0.5) * 8;
                    pos[item.index * 3 + 2] = oz + (Math.random() - 0.5) * 12;
                }
                continue;
            }

            const clusterRadius = 8 + Math.min(items.length * 0.2, 12);
            
            for (let j = 0; j < items.length; j++) {
                const item = items[j];
                
                const goldenAngle = Math.PI * (3 - Math.sqrt(5));
                const theta = goldenAngle * j + (Math.random() - 0.5) * 0.5;
                
                const r = clusterRadius * (0.3 + 0.7 * Math.sqrt(j / Math.max(items.length, 1)));
                
                const jitterX = (Math.random() - 0.5) * 3;
                const jitterY = (Math.random() - 0.5) * 3;
                const jitterZ = (Math.random() - 0.5) * 3;
                
                const yScale = 0.4 + Math.random() * 0.4;
                
                const cx = parentProject.x + Math.cos(theta) * r + jitterX;
                const cy = parentProject.y + (Math.sin(theta) * r * yScale) + jitterY;
                const cz = parentProject.z + (Math.sin(theta * 1.3) * r * 0.7) + jitterZ;

                pos[item.index * 3] = cx;
                pos[item.index * 3 + 1] = cy;
                pos[item.index * 3 + 2] = cz;
            }
        }

        // ─── STEP 6: Store as static positions ───
        this.forcePhysics = {
            count,
            positions: pos,
            step() { /* No-op */ },
            getPositions() { return this.positions; },
            getPosition(idx) {
                return {
                    x: this.positions[idx * 3],
                    y: this.positions[idx * 3 + 1],
                    z: this.positions[idx * 3 + 2]
                };
            }
        };

        console.log('[Brain3D] Organic layout initialized:', count, 'nodes,', projects.length, 'projects,', nsGroups.size, 'namespace clusters');
    }
    // Update edge positions from force physics
    // TubeGeometry edges are static; they rebuild on layout changes only
    updateEdgePositions(positions) {
        // Static layout: edges are fixed, no need to update positions
    }

    createEdges(edgeData) {
        console.log('[Brain3D] createEdges called with', edgeData.length, 'edges');
        this.edges = edgeData;

        // Remove old edges
        this.edgeMeshes.forEach(m => this.scene.remove(m));
        this.edgeMeshes = [];
        
        // Get positions from force physics if available
        const pos = this.forcePhysics ? this.forcePhysics.getPositions() : null;
        if (!pos) return;

        // Group edges by type for batch rendering
        const edgesByType = {};
        for (const edge of edgeData) {
            const type = edge.edge_type || 'relates_to';
            if (!edgesByType[type]) edgesByType[type] = [];
            edgesByType[type].push(edge);
        }

        // Create thin line edges per type (Understand-Anything style)
        for (const [type, edges] of Object.entries(edgesByType)) {
            const typeIdx = this.edgeTypeToIndex(type);
            const typeColor = EDGE_COLORS[typeIdx] || EDGE_COLORS[2];
            const color = new THREE.Color(typeColor);

            const linePositions = [];
            const lineColors = [];
            
            for (const edge of edges) {
                const sourceIdx = this.nodes.findIndex(n => n.id === edge.source);
                const targetIdx = this.nodes.findIndex(n => n.id === edge.target);
                if (sourceIdx === -1 || targetIdx === -1) continue;

                const sx = pos[sourceIdx * 3];
                const sy = pos[sourceIdx * 3 + 1];
                const sz = pos[sourceIdx * 3 + 2];
                const tx = pos[targetIdx * 3];
                const ty = pos[targetIdx * 3 + 1];
                const tz = pos[targetIdx * 3 + 2];

                // Skip NaN positions
                if (isNaN(sx) || isNaN(sy) || isNaN(sz) || isNaN(tx) || isNaN(ty) || isNaN(tz)) continue;

                // Simple straight line (cleaner than tubes)
                linePositions.push(sx, sy, sz);
                linePositions.push(tx, ty, tz);
                
                // Color for both vertices
                lineColors.push(color.r, color.g, color.b);
                lineColors.push(color.r, color.g, color.b);
            }

            if (linePositions.length === 0) continue;

            const geometry = new THREE.BufferGeometry();
            geometry.setAttribute('position', new THREE.Float32BufferAttribute(linePositions, 3));
            geometry.setAttribute('color', new THREE.Float32BufferAttribute(lineColors, 3));

            const material = new THREE.LineBasicMaterial({
                vertexColors: true,
                transparent: true,
                opacity: 0.15,
                blending: THREE.AdditiveBlending,
                depthWrite: false,
            });
            
            const mesh = new THREE.LineSegments(geometry, material);
            mesh.userData.edgeType = type;
            this.edgeMeshes.push(mesh);
            this.scene.add(mesh);
        }
        
        console.log('[Brain3D] Created', this.edgeMeshes.length, 'edge line groups');

        // Create animated edge particles
        this._createEdgeParticles(edgeData, pos);
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // ANIMATED EDGE PARTICLES — Data flow visualization
    // ═══════════════════════════════════════════════════════════════════════════
    _createEdgeParticles(edgeData, pos) {
        // Remove old particle system
        if (this.particleSystem) {
            this.scene.remove(this.particleSystem);
            this.particleSystem.geometry.dispose();
            this.particleSystem.material.dispose();
            this.particleSystem = null;
        }

        if (!edgeData.length || !pos) return;

        const MAX_PARTICLES = Math.min(edgeData.length * 2, 600);
        const geometry = new THREE.BufferGeometry();
        const pPositions = new Float32Array(MAX_PARTICLES * 3);
        const pProgress = new Float32Array(MAX_PARTICLES);
        const pEdgeIndex = new Float32Array(MAX_PARTICLES);
        const pSpeed = new Float32Array(MAX_PARTICLES);
        const pSize = new Float32Array(MAX_PARTICLES);

        // Build valid edge list (only edges with valid node indices)
        const validEdges = [];
        for (let e = 0; e < edgeData.length; e++) {
            const edge = edgeData[e];
            const srcIdx = this.nodes.findIndex(n => n.id === edge.source);
            const tgtIdx = this.nodes.findIndex(n => n.id === edge.target);
            if (srcIdx !== -1 && tgtIdx !== -1) {
                validEdges.push({ srcIdx, tgtIdx, edgeIdx: e, type: edge.edge_type || 'relates_to' });
            }
        }

        if (!validEdges.length) return;

        for (let i = 0; i < MAX_PARTICLES; i++) {
            const ve = validEdges[Math.floor(Math.random() * validEdges.length)];
            pEdgeIndex[i] = ve.edgeIdx;
            pProgress[i] = Math.random();
            pSpeed[i] = 0.3 + Math.random() * 0.7;
            pSize[i] = 1.5 + Math.random() * 2.0;
        }

        geometry.setAttribute('position', new THREE.BufferAttribute(pPositions, 3));
        geometry.setAttribute('aProgress', new THREE.BufferAttribute(pProgress, 1));
        geometry.setAttribute('aEdgeIndex', new THREE.BufferAttribute(pEdgeIndex, 1));
        geometry.setAttribute('aSpeed', new THREE.BufferAttribute(pSpeed, 1));
        geometry.setAttribute('aSize', new THREE.BufferAttribute(pSize, 1));

        // Build edge data texture: RGB = source index, target index, edge type
        const edgeDataArray = new Float32Array(validEdges.length * 4);
        for (let i = 0; i < validEdges.length; i++) {
            const ve = validEdges[i];
            edgeDataArray[i * 4] = ve.srcIdx;
            edgeDataArray[i * 4 + 1] = ve.tgtIdx;
            edgeDataArray[i * 4 + 2] = this.edgeTypeToIndex(ve.type);
            edgeDataArray[i * 4 + 3] = 0;
        }
        const edgeTexture = new THREE.DataTexture(
            edgeDataArray, validEdges.length, 1,
            THREE.RGBAFormat, THREE.FloatType
        );
        edgeTexture.needsUpdate = true;

        this.particleUniforms = {
            uTime: { value: 0 },
            uPositions: { value: pos },
            uEdgeData: { value: edgeTexture },
            uEdgeCount: { value: validEdges.length },
            uParticleCount: { value: MAX_PARTICLES }
        };

        const material = new THREE.ShaderMaterial({
            uniforms: this.particleUniforms,
            vertexShader: `
                uniform float uTime;
                uniform sampler2D uEdgeData;
                uniform float uEdgeCount;
                attribute float aProgress;
                attribute float aEdgeIndex;
                attribute float aSpeed;
                attribute float aSize;
                varying float vAlpha;
                varying vec3 vColor;

                // Edge type colors
                vec3 edgeColor(int type) {
                    if (type == 0) return vec3(0.31, 0.80, 0.77); // depends_on - teal
                    if (type == 1) return vec3(0.31, 0.80, 0.77); // supports - teal
                    if (type == 2) return vec3(0.25, 0.25, 0.25); // relates_to - gray
                    if (type == 3) return vec3(0.31, 0.80, 0.77); // learned_from - teal
                    if (type == 4) return vec3(0.65, 0.55, 0.98); // influences - purple
                    if (type == 5) return vec3(1.0, 0.42, 0.21); // contradicts - orange
                    return vec3(0.5, 0.5, 0.5);
                }

                void main() {
                    // Sample edge data
                    float edgeIdx = floor(aEdgeIndex + 0.5);
                    vec4 edgeInfo = texture2D(uEdgeData, vec2((edgeIdx + 0.5) / uEdgeCount, 0.5));
                    int srcIdx = int(edgeInfo.r);
                    int tgtIdx = int(edgeInfo.g);
                    int eType = int(edgeInfo.b);

                    // Get source and target positions from uPositions (passed as uniform array)
                    // Since we can't easily pass a large array, we'll compute in JS and update attribute
                    // For now, use the position attribute which is updated per-frame
                    vec3 worldPos = position;

                    // Progress along edge with speed
                    float progress = fract(aProgress + uTime * aSpeed * 0.1);

                    // Alpha: fade at both ends
                    float alpha = 1.0 - abs(progress - 0.5) * 2.0;
                    alpha = pow(alpha, 0.7);
                    vAlpha = alpha;
                    vColor = edgeColor(eType);

                    vec4 mvPosition = modelViewMatrix * vec4(worldPos, 1.0);
                    gl_PointSize = aSize * (300.0 / -mvPosition.z);
                    gl_Position = projectionMatrix * mvPosition;
                }
            `,
            fragmentShader: `
                varying float vAlpha;
                varying vec3 vColor;

                void main() {
                    float dist = length(gl_PointCoord - vec2(0.5));
                    if (dist > 0.5) discard;
                    float glow = 1.0 - dist * 2.0;
                    glow = pow(glow, 1.5);
                    gl_FragColor = vec4(vColor, glow * vAlpha * 0.9);
                }
            `,
            transparent: true,
            blending: THREE.AdditiveBlending,
            depthWrite: false
        });

        this.particleSystem = new THREE.Points(geometry, material);
        this.particleSystem.frustumCulled = false;
        this.scene.add(this.particleSystem);

        // Store valid edges for JS-side position updates
        this._validEdges = validEdges;
        this._particleMax = MAX_PARTICLES;

        console.log('[Brain3D] Edge particles created:', MAX_PARTICLES, 'particles on', validEdges.length, 'edges');
    }

    // Update particle positions each frame (JS-side interpolation)
    _updateEdgeParticles(pos, time) {
        if (!this.particleSystem || !this._validEdges) return;

        const positions = this.particleSystem.geometry.attributes.position.array;
        const progressAttr = this.particleSystem.geometry.attributes.aProgress.array;
        const speedAttr = this.particleSystem.geometry.attributes.aSpeed.array;

        for (let i = 0; i < this._particleMax; i++) {
            const edgeIdx = Math.floor(this.particleSystem.geometry.attributes.aEdgeIndex.array[i] + 0.5);
            const ve = this._validEdges.find(e => e.edgeIdx === edgeIdx);
            if (!ve) continue;

            // Advance progress (manual fract since this is JS, not GLSL)
            progressAttr[i] = (progressAttr[i] + speedAttr[i] * 0.008) % 1.0;
            if (progressAttr[i] < 0) progressAttr[i] += 1.0;

            const srcIdx = ve.srcIdx;
            const tgtIdx = ve.tgtIdx;

            const sx = pos[srcIdx * 3];
            const sy = pos[srcIdx * 3 + 1];
            const sz = pos[srcIdx * 3 + 2];
            const tx = pos[tgtIdx * 3];
            const ty = pos[tgtIdx * 3 + 1];
            const tz = pos[tgtIdx * 3 + 2];

            const t = progressAttr[i];
            positions[i * 3] = sx + (tx - sx) * t;
            positions[i * 3 + 1] = sy + (ty - sy) * t;
            positions[i * 3 + 2] = sz + (tz - sz) * t;
        }

        this.particleSystem.geometry.attributes.position.needsUpdate = true;
        this.particleSystem.geometry.attributes.aProgress.needsUpdate = true;
    }
    // Simple geometry merger for edges
    _mergeGeometries(geometries) {
        const merged = new THREE.BufferGeometry();
        const positions = [];
        const normals = [];
        const indices = [];
        let indexOffset = 0;

        for (const geo of geometries) {
            const posAttr = geo.attributes.position;
            const normAttr = geo.attributes.normal;
            const idxAttr = geo.index;

            for (let i = 0; i < posAttr.count; i++) {
                positions.push(posAttr.getX(i), posAttr.getY(i), posAttr.getZ(i));
                if (normAttr) {
                    normals.push(normAttr.getX(i), normAttr.getY(i), normAttr.getZ(i));
                } else {
                    normals.push(0, 0, 1);
                }
            }

            if (idxAttr) {
                for (let i = 0; i < idxAttr.count; i++) {
                    indices.push(idxAttr.getX(i) + indexOffset);
                }
            } else {
                for (let i = 0; i < posAttr.count; i++) {
                    indices.push(indexOffset + i);
                }
            }

            indexOffset += posAttr.count;
        }

        merged.setAttribute('position', new THREE.Float32BufferAttribute(positions, 3));
        merged.setAttribute('normal', new THREE.Float32BufferAttribute(normals, 3));
        merged.setIndex(indices);
        merged.computeVertexNormals();
        return merged;
    }



    // ═══════════════════════════════════════════════════════════════════════════
    // 3D ORBITAL RINGS — like reference's concentric rings but in 3D
    // ═══════════════════════════════════════════════════════════════════════════
    // ═══════════════════════════════════════════════════════════════════════════
    // GLOBAL NAMESPACE VISIBILITY TOGGLE
    // ═══════════════════════════════════════════════════════════════════════════
    setGlobalVisibility(visible) {
        if (!this.nodeGroups) return;
        console.log('[Brain3D] setGlobalVisibility called:', visible);
        
        // Set the flag that updateNodePositions checks every frame
        this._hideGlobalNodes = !visible;
        
        let globalCount = 0;
        for (const node of this.nodes) {
            if (node.namespace === 'global') globalCount++;
        }
        
        console.log('[Brain3D] Global nodes found:', globalCount, '- hidden:', this._hideGlobalNodes);
    }

    onHover(event) {
        const rect = this.renderer.domElement.getBoundingClientRect();
        this.mouse.x = ((event.clientX - rect.left) / rect.width) * 2 - 1;
        this.mouse.y = -((event.clientY - rect.top) / rect.height) * 2 + 1;

        const node = this.raycastNode();
        const tooltip = document.getElementById('tooltip');

        // Notify sidebar
        if (this.onNodeHover) {
            this.onNodeHover(node);
        }

        if (node) {
            const typeColors = {
                decision: '#ff6b35', fact: '#4ecdc4', problem: '#a78bfa',
                preference: '#ff6b35', project: '#4ecdc4', person: '#a78bfa',
                session: '#64748b', event: '#f59e0b'
            };
            const tc = typeColors[node.node_type] || '#6b7b8f';
            const epistemic = node.epistemic_label && node.epistemic_label !== 'unknown' 
                ? `<br><span style="color:#888;">Epistemic:</span> ${node.epistemic_label}` : '';
            const label = (node.label || node.id).substring(0, 60);
            const title = node.node_type === 'session' && node.metadata && node.metadata.topic
                ? `${label} <span style="color:#888;">[${node.metadata.topic}]</span>`
                : label;
            
            tooltip.style.display = 'block';
            tooltip.style.left = (event.clientX + 12) + 'px';
            tooltip.style.top = (event.clientY + 12) + 'px';
            tooltip.innerHTML = `<strong style="color:${tc};">[${node.node_type}]</strong> ${title}${epistemic}<br><span style="color:#888;">Recall: ${node.access_count || 0} · Imp: ${'★'.repeat(Math.round((node.importance || 0.5) * 5))}</span>`;
            this.renderer.domElement.style.cursor = 'pointer';
        } else {
            tooltip.style.display = 'none';
            this.renderer.domElement.style.cursor = 'default';
        }
    }

    onClick(event) {
        const node = this.raycastNode();
        if (node) {
            this.selectedNode = node;
            
            // Notify sidebar
            if (this.onNodeSelect) {
                this.onNodeSelect(node);
            }
            
            // Toggle focus mode: click same node to unfocus
            if (this._focusedNode && this._focusedNode.id === node.id) {
                this._focusedNode = null;
                this.clearHighlight();
                // Reset edge opacity
                this.edgeMeshes.forEach(m => {
                    m.material.opacity = 0.15;
                });
            } else {
                this._focusedNode = node;
                this.highlightNeighbors(node);
                // Highlight edges connected to focused node
                this._highlightFocusEdges(node);
            }
            
            const idx = this.nodes.findIndex(n => n.id === node.id);
            
            // Howard: Trigger measurement collapse
            if (this.collapseAnimations) {
                this.collapseAnimations.set(idx, {
                    startTime: performance.now(),
                    duration: 600,
                    fromScale: 0.5,
                    toScale: 1.0,
                });
                this.propagateCollapse(idx, 0);
            }
            
            this.showNodeInfo(node);
            this.focusNode(node);
        } else {
            // Click background: clear focus
            if (this._focusedNode) {
                this._focusedNode = null;
                this.clearHighlight();
                this.edgeMeshes.forEach(m => {
                    m.material.opacity = 0.15;
                });
            }
        }
    }
    
    // Highlight edges connected to focused node
    _highlightFocusEdges(focusNode) {
        const connectedEdges = new Set();
        for (const edge of this.edges) {
            if (edge.source === focusNode.id || edge.target === focusNode.id) {
                connectedEdges.add(edge.id || `${edge.source}-${edge.target}`);
            }
        }
        
        // Since we batch edges by type, we can't individually highlight edges
        // Instead, increase opacity of all edges (they'll be dimmed by focus mode on nodes)
        this.edgeMeshes.forEach(m => {
            m.material.opacity = 0.4;
        });
    }
    // Howard: Propagate measurement collapse to neighbors
    propagateCollapse(centerIdx, depth) {
        if (depth > 2) return;
        
        const neighborIndices = [];
        for (let i = 0; i < this.edges.length; i++) {
            const edge = this.edges[i];
            const srcIdx = this.nodes.findIndex(n => n.id === edge.source);
            const tgtIdx = this.nodes.findIndex(n => n.id === edge.target);
            if (srcIdx === centerIdx && !this.collapseAnimations.has(tgtIdx)) {
                neighborIndices.push(tgtIdx);
            } else if (tgtIdx === centerIdx && !this.collapseAnimations.has(srcIdx)) {
                neighborIndices.push(srcIdx);
            }
        }
        
        neighborIndices.forEach((ni, i) => {
            setTimeout(() => {
                this.collapseAnimations.set(ni, {
                    startTime: performance.now(),
                    duration: 400,
                    fromScale: 0.7,
                    toScale: 1.0,
                });
                this.propagateCollapse(ni, depth + 1);
            }, 100 + i * 50);
        });
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // VIEW MODES: Full / Micro / Macro / Dissolve
    // ═══════════════════════════════════════════════════════════════════════════
    setViewMode(mode) {
        this._viewMode = mode;
        console.log('[Brain3D] View mode:', mode);
        
        // Update button states
        document.querySelectorAll('.mode-btn').forEach(btn => btn.classList.remove('active'));
        const activeBtn = document.getElementById('mode-' + mode);
        if (activeBtn) activeBtn.classList.add('active');
        
        switch (mode) {
            case 'full':
                this._dissolveTarget = 0;
                this.setSessionVisibility(true);
                this.setEdgeVisibility(true);
                break;
            case 'micro':
                this._dissolveTarget = 0;
                this.setSessionVisibility(false);
                this.setEdgeVisibility(true);
                break;
            case 'macro':
                this._dissolveTarget = 0;
                this.setSessionVisibility(true);
                this.setEdgeVisibility(false);
                break;
            case 'dissolve':
                this._dissolveTarget = 1;
                this.setSessionVisibility(true);
                this.setEdgeVisibility(false);
                break;
        }
    }

    setEdgeVisibility(visible) {
        this.edgeMeshes.forEach(m => m.visible = visible);
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // SEARCH
    // ═══════════════════════════════════════════════════════════════════════════
    searchNodes(query) {
        if (!query || query.length < 2) {
            this.clearHighlight();
            return;
        }
        const q = query.toLowerCase();
        const dummy = new THREE.Object3D();
        
        for (let i = 0; i < this.nodes.length; i++) {
            const node = this.nodes[i];
            const label = (node.label || '').toLowerCase();
            const matches = label.includes(q);
            
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;
            
            const group = this.nodeGroups[mapping.group];
            const mesh = group.children[0];
            const color = new THREE.Color();
            
            if (matches) {
                const c = new THREE.Color(0x00d4aa); // Highlight cyan
                mesh.getColorAt(mapping.local, color);
                mesh.setColorAt(mapping.local, c);
            }
        }
        this.nodeGroups.forEach(g => {
            g.children[0].instanceColor.needsUpdate = true;
        });
    }

    clearHighlight() {
        const color = new THREE.Color();
        for (let i = 0; i < this.nodes.length; i++) {
            const node = this.nodes[i];
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;
            const group = this.nodeGroups[mapping.group];
            const mesh = group.children[0];
            const typeIdx = this.typeToIndex(node.node_type);
            const tc = NODE_COLORS[typeIdx] || NODE_COLORS[6];
            color.setHex(tc);
            mesh.setColorAt(mapping.local, color);
        }
        this.nodeGroups.forEach(g => {
            g.children[0].instanceColor.needsUpdate = true;
        });
    }

    raycastNode() {
        this.raycaster.setFromCamera(this.mouse, this.camera);
        
        for (const group of this.nodeGroups) {
            const mesh = group.children[0]; // Single mesh
            const intersection = this.raycaster.intersectObject(mesh);
            if (intersection.length > 0) {
                const instanceId = intersection[0].instanceId;
                const globalIdx = group.userData.indices[instanceId];
                return this.nodes[globalIdx];
            }
        }
        return null;
    }

    showNodeInfo(node) {
        const panel = document.getElementById('info-panel');
        const content = panel.querySelector('.content');

        const typeColors = {
            decision: '#ff6b35', fact: '#4ecdc4', problem: '#a78bfa',
            preference: '#ff6b35', project: '#4ecdc4', person: '#a78bfa'
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
        if (idx === -1 || !this.forcePhysics) return;
        const positions = this.forcePhysics.getPositions();
        const x = positions[idx * 3];
        const y = positions[idx * 3 + 1];
        const z = positions[idx * 3 + 2];

        const target = new THREE.Vector3(x, y, z);
        
        // Smooth camera transition
        this._animateCameraTo(target, 15); // 15 units away
    }
    
    // Smooth camera animation to a target position
    _animateCameraTo(target, distance) {
        const startPos = this.camera.position.clone();
        const startTarget = this.controls.target.clone();
        
        // Compute end position: offset from target by distance along current view direction
        const direction = new THREE.Vector3().subVectors(startPos, startTarget).normalize();
        const endPos = target.clone().add(direction.multiplyScalar(distance));
        
        const duration = 600; // ms
        const startTime = performance.now();
        
        const animate = () => {
            const elapsed = performance.now() - startTime;
            const progress = Math.min(elapsed / duration, 1);
            const ease = 1 - Math.pow(1 - progress, 3); // ease-out cubic
            
            this.camera.position.lerpVectors(startPos, endPos, ease);
            this.controls.target.lerpVectors(startTarget, target, ease);
            this.controls.update();
            
            if (progress < 1) {
                requestAnimationFrame(animate);
            }
        };
        
        animate();
    }
    highlightNeighbors(node) {
        const neighborIds = new Set();
        this.edges.forEach(e => {
            if (e.source === node.id) neighborIds.add(e.target);
            if (e.target === node.id) neighborIds.add(e.source);
        });

        const color = new THREE.Color();
        
        // Update all node groups (dim non-neighbors)
        for (const group of this.nodeGroups) {
            const mesh = group.children[0];
            const colors = mesh.instanceColor ? mesh.instanceColor.array : null;
            if (!colors) continue;
            
            for (let localIdx = 0; localIdx < group.userData.indices.length; localIdx++) {
                const globalIdx = group.userData.indices[localIdx];
                const n = this.nodes[globalIdx];
                
                if (neighborIds.has(n.id) || n.id === node.id) {
                    const typeIdx = this.typeToIndex(n.node_type);
                    const typeColor = NODE_COLORS[typeIdx] || NODE_COLORS[6];
                    color.setHex(typeColor);
                } else {
                    color.setHex(0x333333);
                }
                
                colors[localIdx * 3] = color.r;
                colors[localIdx * 3 + 1] = color.g;
                colors[localIdx * 3 + 2] = color.b;
            }
            
            mesh.instanceColor.needsUpdate = true;
        }
    }

    setSessionVisibility(visible) {
        this._sessionsVisible = visible;
        this._topicFilter = ''; // Clear topic filter when toggling all sessions
        if (!this.nodeGroups || this.nodeGroups.length === 0) return;
        
        const dummy = new THREE.Object3D();
        
        for (let i = this.projectCount; i < this.nodes.length; i++) {
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;
            
            const group = this.nodeGroups[mapping.group];
            const localIdx = mapping.local;
            const node = this.nodes[i];
            
            if (visible) {
                // Show at normal position - will be updated by updateNodePositions
                const isProject = i < this.projectCount;
                const baseSize = isProject ? 1.2 : 0.15;
                const tierMult = isProject ? this.tierToMult(node.importance || 0.5) : 0.5;
                const size = baseSize * tierMult;
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            } else {
                // Hide by scaling to zero
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        // Mark instance matrices as needing update
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
        });
    }

    setEventVisibility(visible) {
        this._eventsVisible = visible;
        if (!this.nodeGroups || this.nodeGroups.length === 0) return;

        const dummy = new THREE.Object3D();
        for (let i = 0; i < this.nodes.length; i++) {
            const node = this.nodes[i];
            if (node.node_type !== 'event') continue;

            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;

            const group = this.nodeGroups[mapping.group];
            const localIdx = mapping.local;

            if (visible) {
                const baseSize = 0.3 + (node.importance || 0.5) * 0.4;
                const tierMult = this.tierToMult(node.importance || 0.5);
                const size = baseSize * tierMult;
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            } else {
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
        });
    }

    // Toggle sessions on/off — rebuilds the scene with/without session nodes
    toggleSessions(show) {
        if (!this.allNodes || !this.sessionNodes) {
            console.warn('[Brain3D] toggleSessions: no allNodes/sessionNodes data');
            return;
        }
        
        const knowledgeTypes = new Set(['fact', 'decision', 'problem', 'preference', 'advice', 'topic', 'project', 'person', 'concept', 'event', 'agent', 'question']);
        const knowledgeNodes = this.allNodes.filter(n => knowledgeTypes.has(n.node_type));
        
        // Build new node list
        const newNodes = show ? [...knowledgeNodes, ...this.sessionNodes] : knowledgeNodes;
        
        console.log('[Brain3D] toggleSessions:', show, '| Knowledge:', knowledgeNodes.length, '| Sessions:', this.sessionNodes.length, '| Total:', newNodes.length);
        
        // Rebuild the entire visualization
        this.nodes = newNodes;
        this.projectCount = newNodes.filter(n => n.node_type === 'project').length;
        
        // Rebuild node ID map
        const newNodeIdToIndex = {};
        newNodes.forEach((n, i) => newNodeIdToIndex[n.id] = i);
        this.nodeIdToIndex = newNodeIdToIndex;
        
        // Rebuild edges for displayed nodes only
        const displayNodeIds = new Set(newNodes.map(n => n.id));
        // We need allEdges from the original data — stored in brain.allEdges
        const displayEdges = this.allEdges ? this.allEdges.filter(e => displayNodeIds.has(e.source) && displayNodeIds.has(e.target)) : [];
        
        // Clear old scene
        this.edgeMeshes.forEach(m => this.scene.remove(m));
        this.edgeMeshes = [];
        // Recreate nodes
        this.createNodes(newNodes, this.projectCount);
        
        // Recreate physics
        if (this.forcePhysics) {
            this.forcePhysics = null;
        }
        this.initForcePhysics(newNodes, displayEdges, newNodeIdToIndex);
        
        // Recreate edges
        this.createEdges(displayEdges);
                
        // Start activity polling
        this.startActivityPolling(15000);
                
        // Update stats
        if (window.updateStats) {
            window.updateStats(newNodes.length, displayEdges.length, this.projectCount, this.sessionNodes.length);
        }
        
        // Apply current view mode
        if (this._viewMode) {
            this.setViewMode(this._viewMode);
        }
    }

    setTopicFilter(topic) {
        this._topicFilter = topic;
        if (!this.nodeGroups || this.nodeGroups.length === 0) return;
        
        const dummy = new THREE.Object3D();
        
        for (let i = this.projectCount; i < this.nodes.length; i++) {
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;
            
            const group = this.nodeGroups[mapping.group];
            const localIdx = mapping.local;
            const node = this.nodes[i];
            
            const nodeTopic = (node.metadata && node.metadata.topic) ? node.metadata.topic : '';
            const matchesTopic = !topic || nodeTopic === topic;
            
            if (matchesTopic) {
                // Show at normal position
                const isProject = i < this.projectCount;
                const baseSize = isProject ? 1.2 : 0.15;
                const tierMult = isProject ? this.tierToMult(node.importance || 0.5) : 0.5;
                const size = baseSize * tierMult;
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            } else {
                // Hide by scaling to zero
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        // Mark instance matrices as needing update
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
        });
    }

    typeToIndex(t) {
        const map = { decision: 0, fact: 1, problem: 2, preference: 3, project: 4, person: 5 };
        return map[t] || 6;
    }

    edgeTypeToIndex(t) {
        const map = { depends_on: 0, supports: 1, relates_to: 2, learned_from: 3 };
        return map[t] || 2;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // REAL-TIME ACCESS PULSE — Visual feedback on node activity
    // ═══════════════════════════════════════════════════════════════════════════
    triggerPulse(nodeId) {
        const idx = this.nodes.findIndex(n => n.id === nodeId);
        if (idx === -1) return;

        // Add to collapseAnimations for expansion ripple
        this.collapseAnimations.set(idx, {
            startTime: performance.now(),
            duration: 1000,
            fromScale: 1.0,
            toScale: 1.5,
        });
        this.propagateCollapse(idx, 0);

        // Temporarily boost emissiveIntensity on the node's group
        const mapping = this.nodeIndexMap[idx];
        if (mapping) {
            const group = this.nodeGroups[mapping.group];
            const mesh = group.children[0];
            const origIntensity = mesh.material.emissiveIntensity;
            mesh.material.emissiveIntensity = 1.2;
            setTimeout(() => { mesh.material.emissiveIntensity = origIntensity || 0.3; }, 1200);
        }

        console.log('[Brain3D] Pulse triggered for node:', nodeId);
    }

    // Poll for node activity changes
    startActivityPolling(intervalMs = 15000) {
        if (this.pollInterval) clearInterval(this.pollInterval);
        
        const poll = async () => {
            try {
                const resp = await fetch('/api/v1/nodes?limit=200&order_by=access_count&sort=desc');
                if (!resp.ok) return;
                const data = await resp.json();
                if (!Array.isArray(data)) return;

                for (const node of data) {
                    const lastCount = this.lastAccessCounts.get(node.id) || 0;
                    const currentCount = node.access_count || 0;
                    if (currentCount > lastCount) {
                        this.triggerPulse(node.id);
                    }
                    this.lastAccessCounts.set(node.id, currentCount);
                }
            } catch (e) {
                // Silently fail polling
            }
        };

        poll(); // Initial poll
        this.pollInterval = setInterval(poll, intervalMs);
    }

    stopActivityPolling() {
        if (this.pollInterval) {
            clearInterval(this.pollInterval);
            this.pollInterval = null;
        }
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // TOUCH SUPPORT — Pinch zoom and orbit for mobile
    // ═══════════════════════════════════════════════════════════════════════════
    _initTouchSupport() {
        const canvas = this.renderer.domElement;
        let touchStartDist = 0;
        let touchStartCamDist = 0;
        let lastTouchCenter = null;
        let isPinching = false;

        canvas.addEventListener('touchstart', (e) => {
            if (e.touches.length === 2) {
                isPinching = true;
                const dx = e.touches[0].clientX - e.touches[1].clientX;
                const dy = e.touches[0].clientY - e.touches[1].clientY;
                touchStartDist = Math.sqrt(dx * dx + dy * dy);
                touchStartCamDist = this.camera.position.distanceTo(this.controls.target);
                lastTouchCenter = {
                    x: (e.touches[0].clientX + e.touches[1].clientX) / 2,
                    y: (e.touches[0].clientY + e.touches[1].clientY) / 2
                };
            }
        }, { passive: false });

        canvas.addEventListener('touchmove', (e) => {
            if (e.touches.length === 2 && isPinching) {
                e.preventDefault();
                const dx = e.touches[0].clientX - e.touches[1].clientX;
                const dy = e.touches[0].clientY - e.touches[1].clientY;
                const dist = Math.sqrt(dx * dx + dy * dy);
                
                if (touchStartDist > 0) {
                    const scale = touchStartDist / dist;
                    const newDist = Math.max(this.controls.minDistance, 
                        Math.min(this.controls.maxDistance, touchStartCamDist * scale));
                    const dir = new THREE.Vector3()
                        .subVectors(this.camera.position, this.controls.target)
                        .normalize();
                    this.camera.position.copy(this.controls.target).add(dir.multiplyScalar(newDist));
                }

                // Pan with two-finger drag
                const center = {
                    x: (e.touches[0].clientX + e.touches[1].clientX) / 2,
                    y: (e.touches[0].clientY + e.touches[1].clientY) / 2
                };
                if (lastTouchCenter) {
                    const panX = (lastTouchCenter.x - center.x) * 0.05;
                    const panY = (center.y - lastTouchCenter.y) * 0.05;
                    const panOffset = new THREE.Vector3(panX, panY, 0);
                    panOffset.applyQuaternion(this.camera.quaternion);
                    this.camera.position.add(panOffset);
                    this.controls.target.add(panOffset);
                }
                lastTouchCenter = center;
            }
        }, { passive: false });

        canvas.addEventListener('touchend', () => {
            isPinching = false;
            lastTouchCenter = null;
        });

        // Single tap = click (handled by OrbitControls, but we ensure raycast works)
        canvas.addEventListener('touchstart', (e) => {
            if (e.touches.length === 1) {
                const rect = canvas.getBoundingClientRect();
                this.mouse.x = ((e.touches[0].clientX - rect.left) / rect.width) * 2 - 1;
                this.mouse.y = -((e.touches[0].clientY - rect.top) / rect.height) * 2 + 1;
            }
        }, { passive: true });
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // SCREENSHOT CAPTURE
    // ═══════════════════════════════════════════════════════════════════════════
    captureScreenshot() {
        // Render one frame to ensure clean capture
        this.renderer.render(this.scene, this.camera);
        
        const dataURL = this.renderer.domElement.toDataURL('image/png');
        const a = document.createElement('a');
        a.href = dataURL;
        a.download = `mindbank-brain3d-${Date.now()}.png`;
        document.body.appendChild(a);
        a.click();
        document.body.removeChild(a);
        
        console.log('[Brain3D] Screenshot captured');
    }
}
