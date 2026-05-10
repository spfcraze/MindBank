// brain3d.js — Three.js renderer for MindBank 3D graph
import * as THREE from 'three';
import { OrbitControls } from 'three/addons/controls/OrbitControls.js';

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

        // Camera — closer for better visibility
        const aspect = this.container.clientWidth / this.container.clientHeight;
        this.camera = new THREE.PerspectiveCamera(60, aspect, 0.1, 1000);
        this.camera.position.set(0, 0, 15);

        // Renderer
        this.renderer = new THREE.WebGLRenderer({ antialias: true, alpha: false });
        this.renderer.setSize(this.container.clientWidth, this.container.clientHeight);
        this.renderer.setPixelRatio(Math.min(window.devicePixelRatio, 2));
        this.renderer.toneMapping = THREE.ACESFilmicToneMapping;
        this.renderer.toneMappingExposure = 1.2;
        this.container.appendChild(this.renderer.domElement);

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

        // Grid floor
        const gridHelper = new THREE.GridHelper(100, 50, 0x333333, 0x111111);
        gridHelper.position.y = -20;
        this.scene.add(gridHelper);

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
        if (this.composer) {
            this.composer.setSize(w, h);
        }
    }

    animate() {
        if (!this.isRunning) return;
        this.animationId = requestAnimationFrame(() => this.animate());

        const time = performance.now() * 0.001;

        // Grid layout: skip physics, use fixed grid positions
        // Physics is disabled for powergrid layout

        // Update flow particles


        // Update edge signal particles
        this.updateEdgeSignals(time);

        this.controls.update();
        
        // Render with bloom if available
        if (this.composer) {
            this.composer.render();
        } else {
            this.renderer.render(this.scene, this.camera);
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

        // Create node groups with 3-layer glow like reference
        const dummy = new THREE.Object3D();
        const color = new THREE.Color();

        // Group nodes by type for InstancedMesh
        const nodesByType = {};
        for (let i = 0; i < nodeData.length; i++) {
            const type = nodeData[i].node_type || 'unknown';
            if (!nodesByType[type]) nodesByType[type] = [];
            nodesByType[type].push({ node: nodeData[i], index: i });
        }

        // Create 3-layer spheres for each node type
        for (const [type, nodes] of Object.entries(nodesByType)) {
            const group = new THREE.Group();
            group.userData.nodeType = type;
            group.userData.indices = nodes.map(n => n.index);

            const typeIdx = this.typeToIndex(type);
            const typeColor = NODE_COLORS[typeIdx] || NODE_COLORS[6];
            color.setHex(typeColor);

            // Create 3 InstancedMeshes per type: halo, core, bright point
            const count = nodes.length;
            
            // Layer 1: Outer glow halo (4x size, very transparent)
            const haloGeo = new THREE.SphereGeometry(1, 16, 12);
            const haloMat = new THREE.MeshBasicMaterial({
                color: typeColor,
                transparent: true,
                opacity: 0.15,
                depthWrite: false,
                blending: THREE.AdditiveBlending,
            });
            const haloMesh = new THREE.InstancedMesh(haloGeo, haloMat, count);
            haloMesh.userData.layer = 'halo';
            
            // Layer 2: Core sphere (1x size, main body)
            const coreGeo = new THREE.SphereGeometry(1, 16, 12);
            const coreMat = new THREE.MeshBasicMaterial({
                color: typeColor,
                transparent: true,
                opacity: 0.9,
                depthWrite: true,
            });
            const coreMesh = new THREE.InstancedMesh(coreGeo, coreMat, count);
            coreMesh.userData.layer = 'core';
            
            // Layer 3: Inner bright point (0.3x size, white hot center)
            const brightGeo = new THREE.SphereGeometry(1, 8, 6);
            const brightMat = new THREE.MeshBasicMaterial({
                color: 0xffffff,
                transparent: true,
                opacity: 0.6,
                depthWrite: false,
                blending: THREE.AdditiveBlending,
            });
            const brightMesh = new THREE.InstancedMesh(brightGeo, brightMat, count);
            brightMesh.userData.layer = 'bright';

            // Set initial matrices with visual hierarchy
            for (let i = 0; i < nodes.length; i++) {
                const { node, index } = nodes[i];
                const isProject = index < this.projectCount;
                
                // Projects: large (1.2x base), Sessions: tiny (0.15x base)
                const baseSize = isProject ? 1.2 : 0.15;
                const tierMult = isProject ? this.tierToMult(node.importance || 0.5) : 0.5;
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
                dummy.scale.set(size * 4, size * 4, size * 4);
                dummy.updateMatrix();
                haloMesh.setMatrixAt(i, dummy.matrix);
                haloMesh.setColorAt(i, color);

                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                coreMesh.setMatrixAt(i, dummy.matrix);
                coreMesh.setColorAt(i, color);

                dummy.scale.set(size * 0.3, size * 0.3, size * 0.3);
                dummy.updateMatrix();
                brightMesh.setMatrixAt(i, dummy.matrix);
            }

            haloMesh.instanceMatrix.needsUpdate = true;
            haloMesh.instanceColor.needsUpdate = true;
            coreMesh.instanceMatrix.needsUpdate = true;
            coreMesh.instanceColor.needsUpdate = true;
            brightMesh.instanceMatrix.needsUpdate = true;

            group.add(haloMesh);
            group.add(coreMesh);
            group.add(brightMesh);
            
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

        for (let i = 0; i < this.nodes.length; i++) {
            const mapping = this.nodeIndexMap[i];
            if (!mapping) continue;

            const group = this.nodeGroups[mapping.group];
            const node = this.nodes[i];

            // Check if this node should be hidden (global namespace with showGlobal=false)
            const isHidden = this._hideGlobalNodes && node.namespace === 'global';

            const baseSize = 0.3 + (node.importance || 0.5) * 0.4;
            const tierMult = this.tierToMult(node.importance || 0.5);
            const size = baseSize * tierMult;

            if (isHidden) {
                // Move far away and scale to zero
                dummy.position.set(99999, 99999, 99999);
                dummy.rotation.set(0, 0, 0);
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(mapping.local, dummy.matrix);
                group.children[1].setMatrixAt(mapping.local, dummy.matrix);
                group.children[2].setMatrixAt(mapping.local, dummy.matrix);
                continue;
            }

            const x = positions[i * 3];
            const y = positions[i * 3 + 1];
            const z = positions[i * 3 + 2];

            // Pulsing glow like reference: sin(frame * 0.03 + phase) * 0.3 + 0.7
            const pulsePhase = i * 0.5; // Unique phase per node
            const glow = Math.sin(time * 0.03 + pulsePhase) * 0.3 + 0.7;

            dummy.position.set(x, y, z);
            dummy.rotation.set(0, 0, 0);

            // Update halo (4x size, pulses with glow)
            const haloMesh = group.children[0];
            const haloScale = size * 4 * glow;
            dummy.scale.set(haloScale, haloScale, haloScale);
            dummy.updateMatrix();
            haloMesh.setMatrixAt(mapping.local, dummy.matrix);

            // Update core (1x size, pulses with glow)
            const coreMesh = group.children[1];
            const coreScale = size * glow;
            dummy.scale.set(coreScale, coreScale, coreScale);
            dummy.updateMatrix();
            coreMesh.setMatrixAt(mapping.local, dummy.matrix);

            // Update bright point (0.3x size, pulses with glow)
            const brightMesh = group.children[2];
            const brightScale = size * 0.3 * glow;
            dummy.scale.set(brightScale, brightScale, brightScale);
            dummy.updateMatrix();
            brightMesh.setMatrixAt(mapping.local, dummy.matrix);
        }

        this.nodeGroups.forEach(group => {
            group.children.forEach(mesh => {
                mesh.instanceMatrix.needsUpdate = true;
            });
        });
    }

    createEdges(edgeData, nodePositions) {
        console.log('[Brain3D] createEdges called with', edgeData.length, 'edges');
        this.edges = edgeData;

        // Remove old edges
        this.edgeMeshes.forEach(m => this.scene.remove(m));
        this.edgeMeshes = [];
        
        // Remove old signal particles
        if (this.signalParticles) {
            this.scene.remove(this.signalParticles);
            this.signalParticles = null;
        }

        const pos = nodePositions || (this.physics ? this.physics.get_positions() : null);
        if (!pos) return;

        // Build line segments for all edges - glowing synaptic connections
        const lineVertices = [];
        const lineColors = [];

        for (let i = 0; i < edgeData.length; i++) {
            const edge = edgeData[i];
            const sourceIdx = this.nodes.findIndex(n => n.id === edge.source);
            const targetIdx = this.nodes.findIndex(n => n.id === edge.target);
            if (sourceIdx === -1 || targetIdx === -1) continue;

            const sx = pos[sourceIdx * 3];
            const sy = pos[sourceIdx * 3 + 1];
            const sz = pos[sourceIdx * 3 + 2];
            const tx = pos[targetIdx * 3];
            const ty = pos[targetIdx * 3 + 1];
            const tz = pos[targetIdx * 3 + 2];

            const typeIdx = this.edgeTypeToIndex(edge.edge_type);
            const typeColor = EDGE_COLORS[typeIdx] || EDGE_COLORS[2];
            const color = new THREE.Color(typeColor);
            
            // Line segment - glowing synaptic path
            lineVertices.push(sx, sy, sz, tx, ty, tz);
            lineColors.push(color.r, color.g, color.b, color.r, color.g, color.b);
        }

        // Create glowing line connections
        if (lineVertices.length > 0) {
            const lineGeo = new THREE.BufferGeometry();
            lineGeo.setAttribute('position', new THREE.Float32BufferAttribute(lineVertices, 3));
            lineGeo.setAttribute('color', new THREE.Float32BufferAttribute(lineColors, 3));
            
            const lineMat = new THREE.LineBasicMaterial({
                vertexColors: true,
                transparent: true,
                opacity: 0.4,
                blending: THREE.AdditiveBlending,
                depthWrite: false,
            });
            
            const lines = new THREE.LineSegments(lineGeo, lineMat);
            this.edgeMeshes.push(lines);
            this.scene.add(lines);
        }

        // Create traveling signal particles along edges
        this.createEdgeSignals(edgeData, pos);

    }

    createEdgeSignals(edgeData, positions) {
        if (!edgeData || edgeData.length === 0) return;
        
        // Create signal sprites (traveling pulses)
        const signalCount = Math.min(edgeData.length * 2, 300); // Max 300 signals
        const signalPositions = new Float32Array(signalCount * 3);
        const signalColors = new Float32Array(signalCount * 3);
        
        this.signalData = [];
        
        for (let i = 0; i < signalCount; i++) {
            const edgeIdx = i % edgeData.length;
            const edge = edgeData[edgeIdx];
            const sourceIdx = this.nodes.findIndex(n => n.id === edge.source);
            const targetIdx = this.nodes.findIndex(n => n.id === edge.target);
            
            if (sourceIdx === -1 || targetIdx === -1) continue;
            
            // Start at source position
            signalPositions[i * 3] = positions[sourceIdx * 3];
            signalPositions[i * 3 + 1] = positions[sourceIdx * 3 + 1];
            signalPositions[i * 3 + 2] = positions[sourceIdx * 3 + 2];
            
            // Color based on edge type
            const typeIdx = this.edgeTypeToIndex(edge.edge_type);
            const typeColor = EDGE_COLORS[typeIdx] || EDGE_COLORS[2];
            const color = new THREE.Color(typeColor);
            signalColors[i * 3] = color.r;
            signalColors[i * 3 + 1] = color.g;
            signalColors[i * 3 + 2] = color.b;
            
            this.signalData.push({
                edgeIdx,
                sourceIdx,
                targetIdx,
                progress: Math.random(),
                speed: 0.3 + Math.random() * 0.5,
            });
        }
        
        const signalGeometry = new THREE.BufferGeometry();
        signalGeometry.setAttribute('position', new THREE.BufferAttribute(signalPositions, 3));
        signalGeometry.setAttribute('color', new THREE.BufferAttribute(signalColors, 3));
        
        // Use simple points for signals (no texture needed)
        const signalMaterial = new THREE.PointsMaterial({
            size: 0.4,
            blending: THREE.AdditiveBlending,
            depthWrite: false,
            transparent: true,
            opacity: 0.9,
            vertexColors: true,
        });
        
        this.signalParticles = new THREE.Points(signalGeometry, signalMaterial);
        this.scene.add(this.signalParticles);
    }

    updateEdgeSignals(time) {
        if (!this.signalParticles || !this.signalData || this.signalData.length === 0) return;
        
        const positions = this.signalParticles.geometry.attributes.position.array;
        const physicsPositions = this.physics ? this.physics.get_positions() : null;
        
        for (let i = 0; i < this.signalData.length; i++) {
            const sig = this.signalData[i];
            
            // Update progress
            sig.progress += sig.speed * 0.016;
            if (sig.progress >= 1.0) {
                sig.progress = 0;
                // Switch to different edge occasionally
                if (Math.random() < 0.3 && this.edges.length > 0) {
                    sig.edgeIdx = Math.floor(Math.random() * this.edges.length);
                    const edge = this.edges[sig.edgeIdx];
                    sig.sourceIdx = this.nodes.findIndex(n => n.id === edge.source);
                    sig.targetIdx = this.nodes.findIndex(n => n.id === edge.target);
                }
            }
            
            // Get current node positions
            const srcIdx = sig.sourceIdx;
            const tgtIdx = sig.targetIdx;
            if (srcIdx === -1 || tgtIdx === -1) continue;
            
            let sx, sy, sz, tx, ty, tz;
            if (physicsPositions) {
                sx = physicsPositions[srcIdx * 3];
                sy = physicsPositions[srcIdx * 3 + 1];
                sz = physicsPositions[srcIdx * 3 + 2];
                tx = physicsPositions[tgtIdx * 3];
                ty = physicsPositions[tgtIdx * 3 + 1];
                tz = physicsPositions[tgtIdx * 3 + 2];
            } else {
                sx = positions[srcIdx * 3];
                sy = positions[srcIdx * 3 + 1];
                sz = positions[srcIdx * 3 + 2];
                tx = positions[tgtIdx * 3];
                ty = positions[tgtIdx * 3 + 1];
                tz = positions[tgtIdx * 3 + 2];
            }
            
            // Interpolate along edge
            const t = sig.progress;
            positions[i * 3] = sx + (tx - sx) * t;
            positions[i * 3 + 1] = sy + (ty - sy) * t;
            positions[i * 3 + 2] = sz + (tz - sz) * t;
        }
        
        this.signalParticles.geometry.attributes.position.needsUpdate = true;
    }

    // ═══════════════════════════════════════════════════════════════════════════
    // 3D ORBITAL RINGS — like reference's concentric rings but in 3D
    // ═══════════════════════════════════════════════════════════════════════════
    createOrbitalRings() {
        if (this.orbitalRings) {
            this.orbitalRings.forEach(r => this.scene.remove(r));
        }
        this.orbitalRings = [];
        
        const ringConfigs = [
            { radius: 8, color: 0xff6b35, opacity: 0.03 },   // Orange - inner
            { radius: 12, color: 0x4ecdc4, opacity: 0.03 }, // Teal - middle  
            { radius: 16, color: 0xa78bfa, opacity: 0.03 }, // Purple - outer
        ];
        
        for (const config of ringConfigs) {
            // Main ring
            const geometry = new THREE.TorusGeometry(config.radius, 0.02, 8, 64);
            const material = new THREE.MeshBasicMaterial({
                color: config.color,
                transparent: true,
                opacity: config.opacity,
                blending: THREE.AdditiveBlending,
                depthWrite: false,
                side: THREE.DoubleSide,
            });
            const ring = new THREE.Mesh(geometry, material);
            ring.rotation.x = Math.PI / 2;
            this.scene.add(ring);
            this.orbitalRings.push(ring);
            
            // Glow ring (thicker, more transparent)
            const glowGeo = new THREE.TorusGeometry(config.radius, 0.08, 8, 64);
            const glowMat = new THREE.MeshBasicMaterial({
                color: config.color,
                transparent: true,
                opacity: config.opacity * 0.4,
                blending: THREE.AdditiveBlending,
                depthWrite: false,
                side: THREE.DoubleSide,
            });
            const glowRing = new THREE.Mesh(glowGeo, glowMat);
            glowRing.rotation.x = Math.PI / 2;
            this.scene.add(glowRing);
            this.orbitalRings.push(glowRing);
        }
    }

    updateOrbitalRings(time) {
        if (!this.orbitalRings) return;
        
        // Slowly rotate rings
        for (let i = 0; i < this.orbitalRings.length; i += 2) {
            const ring = this.orbitalRings[i];
            const glowRing = this.orbitalRings[i + 1];
            
            // Different rotation speeds per ring
            const speed = 0.02 * (1 + i * 0.3);
            ring.rotation.z += speed * 0.01;
            glowRing.rotation.z += speed * 0.01;
            
            // Subtle tilt oscillation
            ring.rotation.x = Math.PI / 2 + Math.sin(time * 0.1 + i) * 0.05;
            glowRing.rotation.x = Math.PI / 2 + Math.sin(time * 0.1 + i) * 0.05;
        }
    }

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

        if (node) {
            tooltip.style.display = 'block';
            tooltip.style.left = (event.clientX + 10) + 'px';
            tooltip.style.top = (event.clientY + 10) + 'px';
            tooltip.textContent = (node.label || node.id) + ' (' + node.node_type + ')';
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
            this.showNodeInfo(node);
            this.focusNode(node);
        }
    }

    raycastNode() {
        this.raycaster.setFromCamera(this.mouse, this.camera);
        
        // Check all node groups (test against core mesh - index 1)
        for (const group of this.nodeGroups) {
            const coreMesh = group.children[1]; // Core is index 1
            const intersection = this.raycaster.intersectObject(coreMesh);
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
        if (idx === -1 || !this.physics) return;
        const positions = this.physics.get_positions();
        const x = positions[idx * 3];
        const y = positions[idx * 3 + 1];
        const z = positions[idx * 3 + 2];

        const target = new THREE.Vector3(x, y, z);
        this.controls.target.copy(target);
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
            const coreMesh = group.children[1]; // Core is index 1
            const colors = coreMesh.instanceColor ? coreMesh.instanceColor.array : null;
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
            
            coreMesh.instanceColor.needsUpdate = true;
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
                
                dummy.scale.set(size * 4, size * 4, size * 4);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix); // halo
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[1].setMatrixAt(localIdx, dummy.matrix); // core
                
                dummy.scale.set(size * 0.3, size * 0.3, size * 0.3);
                dummy.updateMatrix();
                group.children[2].setMatrixAt(localIdx, dummy.matrix); // bright
            } else {
                // Hide by scaling to zero
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
                group.children[1].setMatrixAt(localIdx, dummy.matrix);
                group.children[2].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        // Mark instance matrices as needing update
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
            group.children[1].instanceMatrix.needsUpdate = true;
            group.children[2].instanceMatrix.needsUpdate = true;
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
                
                dummy.scale.set(size * 4, size * 4, size * 4);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[1].setMatrixAt(localIdx, dummy.matrix);
                
                dummy.scale.set(size * 0.3, size * 0.3, size * 0.3);
                dummy.updateMatrix();
                group.children[2].setMatrixAt(localIdx, dummy.matrix);
            } else {
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
                group.children[1].setMatrixAt(localIdx, dummy.matrix);
                group.children[2].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
            group.children[1].instanceMatrix.needsUpdate = true;
            group.children[2].instanceMatrix.needsUpdate = true;
        });
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
                
                dummy.scale.set(size * 4, size * 4, size * 4);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix); // halo
                
                dummy.scale.set(size, size, size);
                dummy.updateMatrix();
                group.children[1].setMatrixAt(localIdx, dummy.matrix); // core
                
                dummy.scale.set(size * 0.3, size * 0.3, size * 0.3);
                dummy.updateMatrix();
                group.children[2].setMatrixAt(localIdx, dummy.matrix); // bright
            } else {
                // Hide by scaling to zero
                dummy.scale.set(0.001, 0.001, 0.001);
                dummy.updateMatrix();
                group.children[0].setMatrixAt(localIdx, dummy.matrix);
                group.children[1].setMatrixAt(localIdx, dummy.matrix);
                group.children[2].setMatrixAt(localIdx, dummy.matrix);
            }
        }
        
        // Mark instance matrices as needing update
        this.nodeGroups.forEach(group => {
            group.children[0].instanceMatrix.needsUpdate = true;
            group.children[1].instanceMatrix.needsUpdate = true;
            group.children[2].instanceMatrix.needsUpdate = true;
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
}
