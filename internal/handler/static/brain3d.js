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
    0x64748b, // relates_to - visible slate (was 0x404040, nearly invisible)
    0x4ecdc4, // learned_from - teal
    0xa78bfa, // influences - purple
    0xff6b35, // contradicts - orange
    0x808080, // other - gray
    0x3b82f6, // produced - blue (session → knowledge)
    0x3b82f6, // contains - blue
];

// Per-type edge opacity: structural relations read stronger than the
// associative relates_to noise floor, so the graph's real skeleton is
// visible at a glance.
const EDGE_OPACITY = {
    contains: 0.55, produced: 0.55, depends_on: 0.55, decided_by: 0.5,
    contradicts: 0.6, supports: 0.45, learned_from: 0.45, influences: 0.45,
    participated_in: 0.35, temporal_next: 0.3, mentions: 0.25,
    relates_to: 0.22,
};

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
        this.scene.fog = new THREE.FogExp2(0x0a0a1a, 0.004);

        // Camera — better default for large graphs
        const aspect = this.container.clientWidth / this.container.clientHeight;
        this.camera = new THREE.PerspectiveCamera(60, aspect, 0.1, 4000);
        this.camera.position.set(0, 45, 110);

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
                0.8,  // strength — reduced from 1.5 to prevent blown-out glow
                0.5,  // radius
                0.75  // threshold — only bright objects bloom
            );
            this.composer.addPass(this.bloomPass);
            console.log('[Brain3D] Bloom post-processing enabled');
        } else {
            console.log('[Brain3D] Bloom not available — post-processing classes not loaded');
        }

        // Controls — maxDistance is re-fit to the layout's bounding sphere
        // after settle; the old hard cap of 50 trapped the camera inside the
        // graph and made it impossible to see the overall structure.
        this.controls = new OrbitControls(this.camera, this.renderer.domElement);
        this.controls.enableDamping = true;
        this.controls.dampingFactor = 0.08;
        this.controls.minDistance = 2;
        this.controls.maxDistance = 800;
        // Smoother, more responsive navigation and zoom toward the cursor so
        // it's easy to fly to a specific cluster of memories.
        this.controls.rotateSpeed = 0.85;
        this.controls.zoomSpeed = 1.15;
        this.controls.panSpeed = 0.8;
        this.controls.zoomToCursor = true;
        this.controls.screenSpacePanning = true;

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

        // Rewrite instance matrices only when something actually changed —
        // rewriting all N nodes every frame is what pinned FPS into single
        // digits on large graphs. While the layout settles we keep updating;
        // afterward it's driven by a dirty flag set on focus / visibility /
        // dissolve / active collapse animations.
        const dissolveActive = this._dissolveProgress !== this._dissolveTarget;
        const hasActiveCollapse = this.collapseAnimations && this.collapseAnimations.size > 0;
        if (this.forcePhysics && this.forcePhysics.positions) {
            if (this._settling || this._layoutDirty || dissolveActive || hasActiveCollapse) {
                this.updateNodePositions(this.forcePhysics.getPositions());
                this._layoutDirty = false;
            }
        }

        // Update dissolve animation
        if (dissolveActive) {
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

        // Labels are the main CPU cost (per-label DOM writes + CSS2D
        // transforms). Their screen position only changes when the camera or
        // layout moves, so recompute + re-render them only then — this is
        // what lifts a large graph from ~9fps to smooth.
        const cam = this.camera.position, tgt = this.controls.target;
        const camMoved = !this._lastCam ||
            Math.abs(cam.x - this._lastCam[0]) + Math.abs(cam.y - this._lastCam[1]) +
            Math.abs(cam.z - this._lastCam[2]) + Math.abs(tgt.x - this._lastCam[3]) +
            Math.abs(tgt.y - this._lastCam[4]) + Math.abs(tgt.z - this._lastCam[5]) > 0.01;
        const labelsDirty = camMoved || this._settling || this._layoutDirty || this._labelsForceUpdate;
        if (labelsDirty) {
            this._updateLabels();
            this._lastCam = [cam.x, cam.y, cam.z, tgt.x, tgt.y, tgt.z];
            this._labelsForceUpdate = false;
        }

        // Render with bloom if available
        if (this.composer) {
            this.composer.render();
        } else {
            this.renderer.render(this.scene, this.camera);
        }

        // Render labels only when they changed (CSS2D transforms every DOM
        // node otherwise)
        if (this.labelRenderer && labelsDirty) {
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
                emissiveIntensity: 0.5,
                roughness: 0.4,
                metalness: 0.6,
                transparent: true,
                opacity: 0.9,
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
                if (isProject) baseSize = 1.0;
                else if (node.node_type === 'session') baseSize = 0.25;
                else if (node.node_type === 'problem') baseSize = 0.4;
                else if (node.node_type === 'decision') baseSize = 0.5;
                else if (node.node_type === 'fact') baseSize = 0.35;
                else baseSize = 0.4;
                
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
        
        // Limit labels to top nodes by importance to avoid DOM overload
        const maxLabels = 200;
        const sortedByImportance = [...nodeData].map((n, i) => ({ node: n, idx: i, imp: n.importance || 0.5 }))
            .sort((a, b) => b.imp - a.imp);
        const labelIndices = new Set(sortedByImportance.slice(0, maxLabels).map(s => s.idx));
        
        for (let i = 0; i < nodeData.length; i++) {
            if (!labelIndices.has(i)) {
                this.labels.push(null); // placeholder for index alignment
                continue;
            }
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
        this._labelsForceUpdate = true;
    }

    _clearLabels() {
        for (const label of this.labels) {
            if (!label) continue;
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
        
        if (!this._lblMatrix) { this._lblMatrix = new THREE.Matrix4(); this._lblPos = new THREE.Vector3(); }
        const matrix = this._lblMatrix;
        const pos = this._lblPos;

        for (const label of this.labels) {
            if (!label) continue; // skip null placeholders
            const idx = label.userData.nodeIndex;
            const mapping = this.nodeIndexMap[idx];
            if (!mapping) continue;

            const group = this.nodeGroups[mapping.group];
            const mesh = group.children[0];
            mesh.getMatrixAt(mapping.local, matrix);
            pos.setFromMatrixPosition(matrix);

            const node = this.nodes[idx];
            label.position.copy(pos);
            label.position.y += (this._nodeRadii ? this._nodeRadii[idx] : 0.6) + 0.6;

            const ds = this._labelDistScale || 1;
            const dist = camPos.distanceTo(pos);

            // Determine if label should be visible
            let showLabel = false;
            let opacity = 0;

            if (node.node_type === 'project') {
                // Projects are always labeled (if close enough)
                showLabel = true;
                opacity = Math.min(1, Math.max(0, (110 * ds - dist) / (55 * ds)));
            } else if (this._focusedNode) {
                // In focus mode: show selected + neighbors
                if (focusNeighborIds.has(node.id)) {
                    showLabel = true;
                    opacity = Math.min(1, Math.max(0, (70 * ds - dist) / (34 * ds)));
                }
            } else if (node.node_type === 'decision' || node.node_type === 'problem') {
                // Decisions/problems labeled when close
                showLabel = dist < 50 * ds;
                opacity = Math.min(1, Math.max(0, (50 * ds - dist) / (24 * ds)));
            } else if (node.node_type === 'session') {
                // Sessions labeled only when very close
                showLabel = dist < 30 * ds;
                opacity = Math.min(1, Math.max(0, (30 * ds - dist) / (16 * ds)));
            } else {
                // Other types: show labels when close
                showLabel = dist < 36 * ds;
                opacity = Math.min(1, Math.max(0, (36 * ds - dist) / (18 * ds)));
            }
            
            // Selected node always labeled
            if (this.selectedNode && this.selectedNode.id === node.id) {
                showLabel = true;
                opacity = 1;
            }

            // Only touch the DOM when the value actually changes, and toggle
            // the element out of the CSS2D layout entirely when hidden so the
            // renderer skips it.
            const finalOp = showLabel ? opacity : 0;
            if (label._lastOp === undefined || Math.abs(label._lastOp - finalOp) > 0.02) {
                label.element.style.opacity = finalOp;
                label.element.style.display = finalOp <= 0.01 ? 'none' : '';
                label._lastOp = finalOp;
            }
        }
    }
    tierToMult(importance) {
        // Map importance (0-1) to tier multiplier like reference
        if (importance >= 0.8) return 1.5;  // Tier 3 - largest
        if (importance >= 0.5) return 1.2;  // Tier 2
        if (importance >= 0.3) return 0.9;  // Tier 1
        return 0.6;  // Tier 0 - smallest
    }

    // Single source of truth for node visual scale, shared by rendering AND
    // layout spacing so nodes are laid out with room for how big they draw.
    // The old formula (base 4-7 x tier x type) made a project glyph ~19
    // world units wide inside clusters of radius 8-20 — that mismatch was
    // the "everything is one blob" bug.
    _nodeScale(node) {
        const imp = node.importance != null ? node.importance : 0.5;
        if (node.node_type === 'project') {
            return 3.0 + imp * 3.0; // 3.0-6.0: clear anchors, not planets
        }
        return (1.1 + imp * 1.1) * this._getTypeSizeMultiplier(node.node_type) * 1.7;
    }

    // Approximate world-space radius of a drawn glyph (glyph geometries are
    // roughly 0.5 units in radius before instance scaling).
    _nodeVisualRadius(node) {
        return 0.55 * this._nodeScale(node);
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

            // Single source of truth for node visibility. The view-mode /
            // toggle handlers just set these flags + mark dirty; this loop
            // does the actual hiding every frame, so nothing gets overwritten.
            let isHidden = this._hideGlobalNodes && node.namespace === 'global';
            if (this._sessionsVisible === false && node.node_type === 'session') isHidden = true;
            if (this._eventsVisible === false && node.node_type === 'event') isHidden = true;
            if (this._topicFilter && node.node_type === 'session') {
                const topic = node.metadata && node.metadata.topic;
                if (topic !== this._topicFilter) isHidden = true;
            }

            // Shared size formula (also drives layout spacing)
            let size = this._nodeScale(node);
            // De-emphasize disconnected nodes so the connected skeleton — the
            // actual reference of how memories relate — reads clearly through
            // the haze of orphans.
            const deg = this._degree ? this._degree[i] : 1;
            const isOrphan = deg === 0 && node.node_type !== 'project';
            if (isOrphan) size *= 0.6;

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

            // Static scale (no per-frame breathing — reads as noise at scale
            // and forced a full matrix rewrite every frame).
            const glow = 1;

            dummy.position.set(x, y, z);

            // Fixed per-node orientation, baked once at creation, so glyphs
            // look varied without a continuous spin that costs a full matrix
            // rewrite every frame.
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
            
            // Update material opacity for focus mode. Note: material is
            // shared per type, so this sets the type's opacity from the last
            // node processed — orphan dimming is applied via per-instance
            // scale above; connected nodes keep full opacity.
            mesh.material.opacity = 0.95 * dissolveOpacity * focusOpacity;
        }

        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
        });
    }

    // Type-based size multiplier (Understand-Anything hierarchical sizing)
    _getTypeSizeMultiplier(nodeType) {
        switch (nodeType) {
            case 'project': return 1.8;
            case 'decision': return 1.0;
            // Problems are the most numerous type; keep them small so they
            // don't overwhelm the graph (was 0.7, second only to project).
            case 'problem': return 0.45;
            case 'advice': return 0.5;
            case 'preference': return 0.5;
            case 'fact': return 0.4;
            case 'topic': return 0.6;
            case 'concept': return 0.6;
            case 'event': return 0.5;
            case 'agent': return 0.7;
            case 'person': return 0.7;
            case 'question': return 0.6;
            case 'session': return 0.3;
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
    // ─────────────────────────────────────────────────────────────────────
    // EDGE-AWARE FORCE LAYOUT
    // The old layout placed nodes in tight golden-spiral balls per namespace
    // and never looked at edges, so connected memories landed on opposite
    // sides of the scene and every connection was a long chord through the
    // middle. This simulation makes edges springs (connected memories pull
    // together), applies short-range repulsion + collision so nothing
    // bunches, and keeps namespace clusters coherent with a weak cohesion
    // force. It settles live over ~2s, then edges are built at the final
    // positions and the camera fits the result.
    // ─────────────────────────────────────────────────────────────────────
    initForcePhysics(nodes, edges, nodeIdToIndex) {
        const count = nodes.length;
        const pos = new Float32Array(count * 3);

        // Visual radii drive spacing so layout matches what is drawn
        const radii = new Float32Array(count);
        for (let i = 0; i < count; i++) radii[i] = this._nodeVisualRadius(nodes[i]);
        this._nodeRadii = radii;

        // ── Springs from edges (deduped, typed rest lengths) ──
        const springs = [];
        const degree = new Float32Array(count);
        this._degree = degree; // used by rendering to de-emphasize orphans
        const seen = new Set();
        for (const edge of edges) {
            const a = nodeIdToIndex[edge.source];
            const b = nodeIdToIndex[edge.target];
            if (a === undefined || b === undefined || a === b) continue;
            const key = a < b ? a * count + b : b * count + a;
            if (seen.has(key)) continue;
            seen.add(key);
            const type = edge.edge_type || 'relates_to';
            // Structural relations hold tight; associative ones are looser
            let len = 10, k = 0.05;
            if (type === 'contains' || type === 'produced') { len = 7; k = 0.08; }
            else if (type === 'depends_on' || type === 'decided_by') { len = 8; k = 0.07; }
            else if (type === 'relates_to' || type === 'mentions') { len = 14; k = 0.025; }
            springs.push({ a, b, rest: radii[a] + radii[b] + len, k });
            degree[a]++; degree[b]++;
        }

        // ── Seed: namespace clusters on a flattened fibonacci shell ──
        const nsGroups = new Map();
        for (let i = 0; i < count; i++) {
            const ns = nodes[i].namespace || 'global';
            if (!nsGroups.has(ns)) nsGroups.set(ns, []);
            nsGroups.get(ns).push(i);
        }
        const nsList = [...nsGroups.values()];
        const golden = Math.PI * (3 - Math.sqrt(5));
        const clusterR = nsList.map(m => 6 + 2.4 * Math.sqrt(m.length));
        const shellR = nsList.length === 1 ? 0 :
            Math.max(45, Math.max(...clusterR) * 1.7, 6.5 * Math.sqrt(count));
        nsList.forEach((members, gi) => {
            const phi = Math.acos(1 - 2 * ((gi + 0.5) / nsList.length));
            const theta = golden * gi;
            const cx = shellR * Math.sin(phi) * Math.cos(theta);
            const cy = shellR * Math.cos(phi) * 0.45;
            const cz = shellR * Math.sin(phi) * Math.sin(theta);
            const cr = clusterR[gi];
            members.forEach((idx, j) => {
                const p2 = Math.acos(1 - 2 * ((j + 0.5) / members.length));
                const t2 = golden * j;
                const r = cr * Math.cbrt((j + 0.5) / members.length);
                pos[idx * 3] = cx + r * Math.sin(p2) * Math.cos(t2);
                pos[idx * 3 + 1] = cy + r * Math.cos(p2) * 0.6;
                pos[idx * 3 + 2] = cz + r * Math.sin(p2) * Math.sin(t2);
            });
        });

        // Hubs and projects anchor; leaves orbit around them
        const invMass = new Float32Array(count);
        for (let i = 0; i < count; i++) {
            const anchor = nodes[i].node_type === 'project' ? 3 : 0;
            invMass[i] = 1 / (1 + anchor + degree[i] * 0.12);
        }

        const nodeNs = nodes.map(n => n.namespace || 'global');
        const vel = new Float32Array(count * 3);
        const force = new Float32Array(count * 3);
        const iterations = count <= 400 ? 320 : count <= 1200 ? 220 : 150;
        const CELL = 14;

        // Spatial hash for short-range repulsion / collisions
        const buildGrid = () => {
            const grid = new Map();
            for (let i = 0; i < count; i++) {
                const key = (Math.floor(pos[i * 3] / CELL) + 512) +
                    (Math.floor(pos[i * 3 + 1] / CELL) + 512) * 1024 +
                    (Math.floor(pos[i * 3 + 2] / CELL) + 512) * 1048576;
                let cell = grid.get(key);
                if (!cell) { cell = []; grid.set(key, cell); }
                cell.push(i);
            }
            return grid;
        };
        const forEachNeighbor = (grid, i, fn) => {
            const ix = Math.floor(pos[i * 3] / CELL) + 512;
            const iy = Math.floor(pos[i * 3 + 1] / CELL) + 512;
            const iz = Math.floor(pos[i * 3 + 2] / CELL) + 512;
            for (let dx = -1; dx <= 1; dx++)
                for (let dy = -1; dy <= 1; dy++)
                    for (let dz = -1; dz <= 1; dz++) {
                        const cell = grid.get((ix + dx) + (iy + dy) * 1024 + (iz + dz) * 1048576);
                        if (!cell) continue;
                        // Cap work in pathological dense cells
                        const stride = cell.length > 80 ? Math.ceil(cell.length / 80) : 1;
                        for (let c = 0; c < cell.length; c += stride) {
                            const j = cell[c];
                            if (j > i) fn(j);
                        }
                    }
        };

        // Namespace centroids for cohesion (refreshed periodically)
        const nsCenter = new Map();
        const refreshCentroids = () => {
            const acc = new Map();
            for (let i = 0; i < count; i++) {
                let a = acc.get(nodeNs[i]);
                if (!a) { a = [0, 0, 0, 0]; acc.set(nodeNs[i], a); }
                a[0] += pos[i * 3]; a[1] += pos[i * 3 + 1]; a[2] += pos[i * 3 + 2]; a[3]++;
            }
            for (const [ns, a] of acc) nsCenter.set(ns, [a[0] / a[3], a[1] / a[3], a[2] / a[3]]);
        };
        refreshCentroids();

        const stepBatch = (from, to) => {
            for (let iter = from; iter < to; iter++) {
                const alpha = Math.pow(0.01, iter / iterations); // 1 → 0.01 cooling
                force.fill(0);

                // Springs: connected memories pull toward their rest length
                for (const s of springs) {
                    const dx = pos[s.b * 3] - pos[s.a * 3];
                    const dy = pos[s.b * 3 + 1] - pos[s.a * 3 + 1];
                    const dz = pos[s.b * 3 + 2] - pos[s.a * 3 + 2];
                    const dist = Math.max(Math.sqrt(dx * dx + dy * dy + dz * dz), 0.01);
                    const f = s.k * (dist - s.rest) / dist * alpha;
                    force[s.a * 3] += dx * f; force[s.a * 3 + 1] += dy * f; force[s.a * 3 + 2] += dz * f;
                    force[s.b * 3] -= dx * f; force[s.b * 3 + 1] -= dy * f; force[s.b * 3 + 2] -= dz * f;
                }

                // Short-range repulsion: nothing bunches or overlaps
                const grid = buildGrid();
                for (let i = 0; i < count; i++) {
                    forEachNeighbor(grid, i, (j) => {
                        const dx = pos[i * 3] - pos[j * 3];
                        const dy = pos[i * 3 + 1] - pos[j * 3 + 1];
                        const dz = pos[i * 3 + 2] - pos[j * 3 + 2];
                        const d2 = Math.max(dx * dx + dy * dy + dz * dz, 0.25);
                        const reach = radii[i] + radii[j] + 12;
                        if (d2 > reach * reach) return;
                        const near = radii[i] + radii[j] + 2;
                        let rep = 2.6 * (near * near) / d2 * alpha;
                        if (rep > 1.5) rep = 1.5;
                        const d = Math.sqrt(d2);
                        const fx = (dx / d) * rep, fy = (dy / d) * rep, fz = (dz / d) * rep;
                        force[i * 3] += fx; force[i * 3 + 1] += fy; force[i * 3 + 2] += fz;
                        force[j * 3] -= fx; force[j * 3 + 1] -= fy; force[j * 3 + 2] -= fz;
                    });
                }

                // Namespace cohesion + gentle global centering / y-flatten
                if (iter % 8 === 0) refreshCentroids();
                for (let i = 0; i < count; i++) {
                    const c = nsCenter.get(nodeNs[i]);
                    if (c) {
                        force[i * 3] += (c[0] - pos[i * 3]) * 0.012 * alpha;
                        force[i * 3 + 1] += (c[1] - pos[i * 3 + 1]) * 0.012 * alpha;
                        force[i * 3 + 2] += (c[2] - pos[i * 3 + 2]) * 0.012 * alpha;
                    }
                    force[i * 3] -= pos[i * 3] * 0.0015 * alpha;
                    force[i * 3 + 1] -= pos[i * 3 + 1] * 0.008 * alpha;
                    force[i * 3 + 2] -= pos[i * 3 + 2] * 0.0015 * alpha;
                }

                // Integrate with damping and a speed cap
                for (let i = 0; i < count; i++) {
                    const im = invMass[i];
                    let vx = (vel[i * 3] + force[i * 3] * im) * 0.82;
                    let vy = (vel[i * 3 + 1] + force[i * 3 + 1] * im) * 0.82;
                    let vz = (vel[i * 3 + 2] + force[i * 3 + 2] * im) * 0.82;
                    const sp = Math.sqrt(vx * vx + vy * vy + vz * vz);
                    if (sp > 2.5) { const s = 2.5 / sp; vx *= s; vy *= s; vz *= s; }
                    vel[i * 3] = vx; vel[i * 3 + 1] = vy; vel[i * 3 + 2] = vz;
                    pos[i * 3] += vx; pos[i * 3 + 1] += vy; pos[i * 3 + 2] += vz;
                }
            }
        };

        // Final hard de-overlap so no two glyphs intersect
        const resolveCollisions = () => {
            for (let pass = 0; pass < 10; pass++) {
                const grid = buildGrid();
                let moved = false;
                for (let i = 0; i < count; i++) {
                    forEachNeighbor(grid, i, (j) => {
                        const dx = pos[j * 3] - pos[i * 3];
                        const dy = pos[j * 3 + 1] - pos[i * 3 + 1];
                        const dz = pos[j * 3 + 2] - pos[i * 3 + 2];
                        const minSep = radii[i] + radii[j] + 0.8;
                        const d2 = dx * dx + dy * dy + dz * dz;
                        if (d2 >= minSep * minSep) return;
                        const d = Math.max(Math.sqrt(d2), 0.01);
                        const push = (minSep - d) / d;
                        const wi = invMass[i] / (invMass[i] + invMass[j]);
                        pos[i * 3] -= dx * push * wi; pos[i * 3 + 1] -= dy * push * wi; pos[i * 3 + 2] -= dz * push * wi;
                        pos[j * 3] += dx * push * (1 - wi); pos[j * 3 + 1] += dy * push * (1 - wi); pos[j * 3 + 2] += dz * push * (1 - wi);
                        moved = true;
                    });
                }
                if (!moved) break;
            }
        };

        this.forcePhysics = {
            count,
            positions: pos,
            step() { /* static after settle */ },
            getPositions() { return this.positions; },
            getPosition(idx) {
                return {
                    x: this.positions[idx * 3],
                    y: this.positions[idx * 3 + 1],
                    z: this.positions[idx * 3 + 2]
                };
            }
        };

        // Settle asynchronously (~14 iterations/frame) so the graph visibly
        // organizes itself, then build edges at final positions + fit camera.
        this._settling = true;
        let iter = 0;
        const runChunk = () => {
            if (!this.isRunning) return;
            const to = Math.min(iter + 14, iterations);
            stepBatch(iter, to);
            iter = to;
            if (iter < iterations) {
                requestAnimationFrame(runChunk);
            } else {
                resolveCollisions();
                this._settling = false;
                this._layoutDirty = true; // one final matrix write at rest
                if (this._pendingEdges) {
                    const e = this._pendingEdges;
                    this._pendingEdges = null;
                    this.createEdges(e);
                }
                this.fitCameraToLayout();
                console.log('[Brain3D] Force layout settled:', count, 'nodes,', springs.length, 'springs,', nsList.length, 'namespace clusters,', iterations, 'iterations');
            }
        };
        requestAnimationFrame(runChunk);

        console.log('[Brain3D] Force layout started:', count, 'nodes,', springs.length, 'springs');
    }

    // Frame the whole layout: aim controls at its centroid, back the camera
    // off to the bounding radius, and widen the zoom-out limit accordingly.
    fitCameraToLayout() {
        if (!this.forcePhysics) return;
        const p = this.forcePhysics.positions;
        const n = this.forcePhysics.count;
        if (!n) return;
        let cx = 0, cy = 0, cz = 0;
        for (let i = 0; i < n; i++) { cx += p[i * 3]; cy += p[i * 3 + 1]; cz += p[i * 3 + 2]; }
        cx /= n; cy /= n; cz /= n;
        // Robust radius: 92nd-percentile distance so a handful of far orphan
        // nodes don't push the camera so far back the dense core looks tiny.
        const dists = new Float32Array(n);
        for (let i = 0; i < n; i++) {
            const dx = p[i * 3] - cx, dy = p[i * 3 + 1] - cy, dz = p[i * 3 + 2] - cz;
            dists[i] = Math.sqrt(dx * dx + dy * dy + dz * dz);
        }
        dists.sort();
        const radius = Math.max(dists[Math.floor(n * 0.90)], 25);

        this.controls.target.set(cx, cy, cz);
        const dir = new THREE.Vector3(0.4, 0.5, 1).normalize();
        this.camera.position.set(cx, cy, cz).addScaledVector(dir, radius * 2.1);
        this.camera.far = Math.max(2000, radius * 12);
        this.camera.updateProjectionMatrix();
        this.controls.maxDistance = radius * 6;
        this.controls.update();
        if (this.scene.fog) this.scene.fog.density = Math.min(0.004, 1.4 / (radius * 8));
        // Label fade distances are tuned for a ~45-unit layout; scale them up
        // for bigger graphs so labels still appear at comfortable zoom levels.
        this._labelDistScale = Math.max(1, radius / 45);
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

        // While the force layout settles, defer building: edges drawn at
        // seed positions would be rebuilt anyway and just flash spaghetti.
        if (this._settling) {
            this._pendingEdges = edgeData;
            return;
        }

        // Get positions from force physics if available
        const pos = this.forcePhysics ? this.forcePhysics.getPositions() : null;
        if (!pos) return;

        const idToIndex = this.nodeIdToIndex || {};
        const radii = this._nodeRadii;
        const SEGMENTS = 8;
        // Arc control points, keyed by index into edgeData, shared with the
        // flow particles so they travel along the drawn curve.
        this._edgeArcs = new Map();

        // Group edges by type for batch rendering
        const edgesByType = {};
        for (let e = 0; e < edgeData.length; e++) {
            const type = edgeData[e].edge_type || 'relates_to';
            if (!edgesByType[type]) edgesByType[type] = [];
            edgesByType[type].push(e);
        }

        const up = new THREE.Vector3(0, 1, 0);
        const dir = new THREE.Vector3();
        const perp = new THREE.Vector3();

        for (const [type, edgeIndices] of Object.entries(edgesByType)) {
            const typeIdx = this.edgeTypeToIndex(type);
            const typeColor = EDGE_COLORS[typeIdx] || EDGE_COLORS[2];
            const color = new THREE.Color(typeColor);

            const linePositions = [];
            const lineColors = [];

            for (const e of edgeIndices) {
                const edge = edgeData[e];
                const a = idToIndex[edge.source];
                const b = idToIndex[edge.target];
                if (a === undefined || b === undefined || a === b) continue;

                const sx = pos[a * 3], sy = pos[a * 3 + 1], sz = pos[a * 3 + 2];
                const tx = pos[b * 3], ty = pos[b * 3 + 1], tz = pos[b * 3 + 2];
                if (isNaN(sx) || isNaN(tx)) continue;

                const dx = tx - sx, dy = ty - sy, dz = tz - sz;
                const len = Math.sqrt(dx * dx + dy * dy + dz * dz);
                if (len < 0.01) continue;

                // Gentle arc perpendicular to the edge: separates parallel
                // edges and keeps lines from slicing through cluster cores.
                dir.set(dx / len, dy / len, dz / len);
                perp.crossVectors(dir, up);
                if (perp.lengthSq() < 1e-4) perp.set(1, 0, 0);
                perp.normalize();
                const side = ((a + b) % 2 === 0) ? 1 : -1; // alternate bow side
                const bow = Math.min(len * 0.12, 4);
                const mx = (sx + tx) / 2 + perp.x * bow * side;
                const my = (sy + ty) / 2 + bow * 0.5;
                const mz = (sz + tz) / 2 + perp.z * bow * side;

                // Trim endpoints to glyph surfaces so lines connect visibly
                // to shapes instead of vanishing inside them.
                const rA = radii ? radii[a] : 0.6;
                const rB = radii ? radii[b] : 0.6;
                let t0 = Math.min((rA + 0.3) / len, 0.4);
                let t1 = 1 - Math.min((rB + 0.3) / len, 0.4);
                if (t1 <= t0) { t0 = 0; t1 = 1; }

                // Per-edge brightness from weight
                const w = edge.weight != null ? edge.weight : 1;
                const bright = Math.min(0.65 + Math.min(w, 1.5) * 0.35, 1.2);
                const cr = Math.min(color.r * bright, 1);
                const cg = Math.min(color.g * bright, 1);
                const cb = Math.min(color.b * bright, 1);

                let px = 0, py = 0, pz = 0;
                for (let s = 0; s <= SEGMENTS; s++) {
                    const t = t0 + (t1 - t0) * (s / SEGMENTS);
                    const u = 1 - t;
                    const qx = u * u * sx + 2 * u * t * mx + t * t * tx;
                    const qy = u * u * sy + 2 * u * t * my + t * t * ty;
                    const qz = u * u * sz + 2 * u * t * mz + t * t * tz;
                    if (s > 0) {
                        linePositions.push(px, py, pz, qx, qy, qz);
                        lineColors.push(cr, cg, cb, cr, cg, cb);
                    }
                    px = qx; py = qy; pz = qz;
                }

                this._edgeArcs.set(e, { a, b, mx, my, mz, t0, t1 });
            }

            if (linePositions.length === 0) continue;

            const geometry = new THREE.BufferGeometry();
            geometry.setAttribute('position', new THREE.Float32BufferAttribute(linePositions, 3));
            geometry.setAttribute('color', new THREE.Float32BufferAttribute(lineColors, 3));

            const material = new THREE.LineBasicMaterial({
                vertexColors: true,
                transparent: true,
                opacity: EDGE_OPACITY[type] !== undefined ? EDGE_OPACITY[type] : 0.35,
                blending: THREE.AdditiveBlending,
                depthWrite: false,
            });

            const mesh = new THREE.LineSegments(geometry, material);
            mesh.userData.edgeType = type;
            mesh.userData.baseOpacity = material.opacity;
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
        const idToIndex = this.nodeIdToIndex || {};
        const validEdges = [];
        for (let e = 0; e < edgeData.length; e++) {
            const edge = edgeData[e];
            const srcIdx = idToIndex[edge.source];
            const tgtIdx = idToIndex[edge.target];
            if (srcIdx !== undefined && tgtIdx !== undefined && srcIdx !== tgtIdx) {
                validEdges.push({
                    srcIdx, tgtIdx, edgeIdx: e,
                    type: edge.edge_type || 'relates_to',
                    arc: this._edgeArcs ? this._edgeArcs.get(e) : null,
                });
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

        // Store valid edges for JS-side position updates, indexed by edge
        // number so the per-frame update is a map lookup instead of a scan.
        this._validEdges = validEdges;
        this._validEdgeByIdx = new Map(validEdges.map(ve => [ve.edgeIdx, ve]));
        this._particleMax = MAX_PARTICLES;

        console.log('[Brain3D] Edge particles created:', MAX_PARTICLES, 'particles on', validEdges.length, 'edges');
    }

    // Update particle positions each frame (JS-side interpolation)
    _updateEdgeParticles(pos, time) {
        if (!this.particleSystem || !this._validEdges) return;

        const positions = this.particleSystem.geometry.attributes.position.array;
        const progressAttr = this.particleSystem.geometry.attributes.aProgress.array;
        const speedAttr = this.particleSystem.geometry.attributes.aSpeed.array;

        const edgeIdxAttr = this.particleSystem.geometry.attributes.aEdgeIndex.array;
        for (let i = 0; i < this._particleMax; i++) {
            const edgeIdx = Math.floor(edgeIdxAttr[i] + 0.5);
            const ve = this._validEdgeByIdx ? this._validEdgeByIdx.get(edgeIdx) : null;
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

            // Follow the drawn arc (quadratic bezier) so flow particles ride
            // the visible line instead of cutting a straight chord.
            const arc = ve.arc;
            if (arc) {
                const t = arc.t0 + (arc.t1 - arc.t0) * progressAttr[i];
                const u = 1 - t;
                positions[i * 3] = u * u * sx + 2 * u * t * arc.mx + t * t * tx;
                positions[i * 3 + 1] = u * u * sy + 2 * u * t * arc.my + t * t * ty;
                positions[i * 3 + 2] = u * u * sz + 2 * u * t * arc.mz + t * t * tz;
            } else {
                const t = progressAttr[i];
                positions[i * 3] = sx + (tx - sx) * t;
                positions[i * 3 + 1] = sy + (ty - sy) * t;
                positions[i * 3 + 2] = sz + (tz - sz) * t;
            }
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
        this._layoutDirty = true;
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
        this._lastPointer = { x: event.clientX, y: event.clientY };

        const node = this._pickNode(event.clientX, event.clientY);
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
        const node = this._pickNode(event.clientX, event.clientY);
        if (node) {
            this.selectedNode = node;

            // Notify sidebar
            if (this.onNodeSelect) {
                this.onNodeSelect(node);
            }
            
            // Toggle focus mode: click same node to unfocus
            if (this._focusedNode && this._focusedNode.id === node.id) {
                this._focusedNode = null;
                this._layoutDirty = true;
                this.clearHighlight();
                // Restore each edge type's own base opacity
                this.edgeMeshes.forEach(m => {
                    m.material.opacity = m.userData.baseOpacity !== undefined ? m.userData.baseOpacity : 0.35;
                });
            } else {
                this._focusedNode = node;
                this._layoutDirty = true;
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
            
            // Node details are shown via the onNodeSelect callback, which
            // populates the sidebar "🔍 Node Details" panel — the same format
            // as hover. (Previously this also opened a separate #info-panel
            // with a different layout, which was inconsistent.)
            this.focusNode(node);
            return;
        }

        // No node — try to select an edge (connection)
        const edge = this._pickEdge(event.clientX, event.clientY);
        if (edge) {
            this._showEdgeInfo(edge, event);
            return;
        }

        // Click empty background: clear focus
        if (this._focusedNode) {
            this._focusedNode = null;
            this._layoutDirty = true;
            this.clearHighlight();
            this.edgeMeshes.forEach(m => {
                m.material.opacity = m.userData.baseOpacity !== undefined ? m.userData.baseOpacity : 0.35;
            });
        }
    }

    // Show a clicked edge: labels of both endpoints + relation type, and pulse
    // the two connected nodes so the connection is obvious.
    _showEdgeInfo(edge, event) {
        const si = this.nodeIdToIndex ? this.nodeIdToIndex[edge.source] : undefined;
        const ti = this.nodeIdToIndex ? this.nodeIdToIndex[edge.target] : undefined;
        const src = si !== undefined ? this.nodes[si] : null;
        const tgt = ti !== undefined ? this.nodes[ti] : null;
        const name = n => this.escapeHtml((n ? (n.label || n.id) : 'unknown').substring(0, 40));
        const type = this.escapeHtml((edge.edge_type || 'relates_to').replace(/_/g, ' '));
        const tooltip = document.getElementById('tooltip');
        if (tooltip && event) {
            tooltip.style.display = 'block';
            tooltip.style.left = (event.clientX + 12) + 'px';
            tooltip.style.top = (event.clientY + 12) + 'px';
            tooltip.innerHTML = `<span style="color:#888;">edge</span> <strong style="color:#00D4AA;">${type}</strong>`
                + `<br>${name(src)} <span style="color:#888;">&rarr;</span> ${name(tgt)}`;
        }
        if (src) this.triggerPulse(src.id);
        if (tgt) this.triggerPulse(tgt.id);
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
        // Instead, boost each type above its base opacity (nodes outside the
        // focus neighborhood are dimmed separately)
        this.edgeMeshes.forEach(m => {
            const base = m.userData.baseOpacity !== undefined ? m.userData.baseOpacity : 0.35;
            m.material.opacity = Math.min(base * 1.6, 0.8);
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
        // Also hide the flow particles, otherwise dots keep streaming along
        // invisible edges in Macro/Dissolve modes.
        if (this.particleSystem) this.particleSystem.visible = visible;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // SEARCH
    // ═══════════════════════════════════════════════════════════════════════════
    searchNodes(query) {
        this._layoutDirty = true;
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
        this._layoutDirty = true;
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

    // Screen-space node picker: projects every visible node to the screen and
    // returns the one nearest the cursor within a generous, size-aware hit
    // radius. Exact-mesh raycasting made the small glyphs very hard to click;
    // this lets you click *near* a node and reliably hit it, preferring the
    // one closest to the cursor and nearest the camera on ties.
    _pickNode(clientX, clientY) {
        if (!this.forcePhysics || !this.nodes || !this.nodes.length) return null;
        const rect = this.renderer.domElement.getBoundingClientRect();
        const px = clientX - rect.left, py = clientY - rect.top;
        const pos = this.forcePhysics.getPositions();
        const v = this._pickVec || (this._pickVec = new THREE.Vector3());
        const halfH = rect.height / 2;
        const focalPx = halfH / Math.tan((this.camera.fov * Math.PI / 180) / 2);
        const camPos = this.camera.position;
        let best = null, bestScore = Infinity;
        for (let i = 0; i < this.nodes.length; i++) {
            const node = this.nodes[i];
            if (this._hideGlobalNodes && node.namespace === 'global') continue;
            const wx = pos[i * 3], wy = pos[i * 3 + 1], wz = pos[i * 3 + 2];
            v.set(wx, wy, wz).project(this.camera);
            if (v.z > 1) continue; // behind camera / clipped
            const sx = (v.x * 0.5 + 0.5) * rect.width;
            const sy = (-v.y * 0.5 + 0.5) * rect.height;
            const dx = sx - px, dy = sy - py;
            const d = Math.sqrt(dx * dx + dy * dy);
            const distCam = Math.hypot(wx - camPos.x, wy - camPos.y, wz - camPos.z) || 1;
            const projR = (this._nodeVisualRadius(node) / distCam) * focalPx;
            const hitR = Math.max(projR, 5) + 7; // padding for easy clicking
            if (d > hitR) continue;
            // Prefer nearest to cursor, then nearest to camera (front-most)
            const score = d - projR * 0.6 + v.z * 8;
            if (score < bestScore) { bestScore = score; best = node; }
        }
        return best;
    }

    // Screen-space edge picker: samples each drawn arc, projects the samples,
    // and returns the edge whose curve passes closest to the cursor (within a
    // few px). Lets users click connections, not just nodes.
    _pickEdge(clientX, clientY) {
        if (!this.forcePhysics || !this.edges || !this._edgeArcs) return null;
        const rect = this.renderer.domElement.getBoundingClientRect();
        const px = clientX - rect.left, py = clientY - rect.top;
        const pos = this.forcePhysics.getPositions();
        const v = this._pickVec2 || (this._pickVec2 = new THREE.Vector3());
        const idToIndex = this.nodeIdToIndex || {};
        const THRESH = 8; // px
        let best = null, bestD = THRESH;
        const project = (x, y, z) => {
            v.set(x, y, z).project(this.camera);
            if (v.z > 1) return null;
            return [(v.x * 0.5 + 0.5) * rect.width, (-v.y * 0.5 + 0.5) * rect.height];
        };
        for (let e = 0; e < this.edges.length; e++) {
            const arc = this._edgeArcs.get(e);
            if (!arc) continue;
            const a = arc.a, b = arc.b;
            const sx = pos[a * 3], sy = pos[a * 3 + 1], sz = pos[a * 3 + 2];
            const tx = pos[b * 3], ty = pos[b * 3 + 1], tz = pos[b * 3 + 2];
            let prev = null;
            const SEG = 6;
            for (let s = 0; s <= SEG; s++) {
                const t = arc.t0 + (arc.t1 - arc.t0) * (s / SEG);
                const u = 1 - t;
                const qx = u * u * sx + 2 * u * t * arc.mx + t * t * tx;
                const qy = u * u * sy + 2 * u * t * arc.my + t * t * ty;
                const qz = u * u * sz + 2 * u * t * arc.mz + t * t * tz;
                const p = project(qx, qy, qz);
                if (p && prev) {
                    const d = this._distToSegment(px, py, prev[0], prev[1], p[0], p[1]);
                    if (d < bestD) { bestD = d; best = this.edges[e]; }
                }
                prev = p;
            }
        }
        return best;
    }

    _distToSegment(px, py, x1, y1, x2, y2) {
        const dx = x2 - x1, dy = y2 - y1;
        const len2 = dx * dx + dy * dy;
        let t = len2 ? ((px - x1) * dx + (py - y1) * dy) / len2 : 0;
        t = Math.max(0, Math.min(1, t));
        const cx = x1 + t * dx, cy = y1 + t * dy;
        return Math.hypot(px - cx, py - cy);
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

        // Smooth camera transition — stand back proportionally to the node's
        // visual size so big projects and small facts both frame nicely.
        const r = this._nodeRadii ? this._nodeRadii[idx] : 1;
        this._animateCameraTo(target, Math.max(15, r * 9));
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
        this._layoutDirty = true;
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

    // Session/event visibility is a flag read by updateNodePositions each
    // frame — set it and mark dirty. (The old versions wrote instance
    // matrices directly, which updateNodePositions overwrote every frame, so
    // the toggles didn't actually work; setSessionVisibility also hid *all*
    // non-project nodes, not just sessions.)
    setSessionVisibility(visible) {
        this._sessionsVisible = visible;
        this._topicFilter = ''; // Clear topic filter when toggling all sessions
        this._layoutDirty = true;
    }

    setEventVisibility(visible) {
        this._eventsVisible = visible;
        this._layoutDirty = true;
    }

    // Toggle sessions on/off — rebuilds the scene with/without session nodes
    toggleSessions(show) {
        if (!this.allNodes) {
            console.warn('[Brain3D] toggleSessions: no allNodes data');
            return;
        }

        // Derive the full node list from allNodes (the complete dataset,
        // including synthesized session nodes) so re-checking the box restores
        // every session. The previous version added back a stale sessionNodes
        // snapshot captured before synthesis, so sessions never came back.
        const newNodes = show
            ? this.allNodes.slice()
            : this.allNodes.filter(n => n.node_type !== 'session');

        console.log('[Brain3D] toggleSessions:', show, '| Total:', newNodes.length);
        
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
            const sessionCount = newNodes.filter(n => n.node_type === 'session').length;
            window.updateStats(newNodes.length, displayEdges.length, this.projectCount, sessionCount);
        }
        
        // Apply current view mode
        if (this._viewMode) {
            this.setViewMode(this._viewMode);
        }
    }

    // Topic filter is read by updateNodePositions (sessions whose topic
    // doesn't match are hidden). Selecting a topic implies showing sessions.
    setTopicFilter(topic) {
        this._topicFilter = topic;
        if (topic) this._sessionsVisible = true;
        this._layoutDirty = true;
    }

    typeToIndex(t) {
        const map = { decision: 0, fact: 1, problem: 2, preference: 3, project: 4, person: 5 };
        return map[t] || 6;
    }

    edgeTypeToIndex(t) {
        const map = {
            depends_on: 0, supports: 1, relates_to: 2, learned_from: 3,
            influences: 4, contradicts: 5, produced: 7, contains: 8
        };
        return map[t] !== undefined ? map[t] : 6;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // REAL-TIME ACCESS PULSE — Visual feedback on node activity
    // ═══════════════════════════════════════════════════════════════════════════
    triggerPulse(nodeId) {
        this._layoutDirty = true;
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
