// brain3d-glyphs.js — 3D Neural Glyph Geometry Factory
// Procedural BufferGeometry generators for 12 MindBank node types

import * as THREE from 'three';

/**
 * Merge multiple BufferGeometries into one.
 * Simple merge without matrix transforms (assumes pre-positioned).
 */
function mergeGeometries(geometries) {
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

/**
 * Create a beveled box by subdividing and pushing corner vertices inward.
 */
function createBeveledBox(size, bevel = 0.1) {
    const geo = new THREE.BoxGeometry(size, size, size, 2, 2, 2);
    const pos = geo.attributes.position;
    const half = size / 2;

    for (let i = 0; i < pos.count; i++) {
        const x = pos.getX(i);
        const y = pos.getY(i);
        const z = pos.getZ(i);

        const dx = Math.abs(x) - half + bevel;
        const dy = Math.abs(y) - half + bevel;
        const dz = Math.abs(z) - half + bevel;

        if (dx > 0 && dy > 0 && dz > 0) {
            const factor = 1 - (bevel / size);
            pos.setXYZ(i, x * factor, y * factor, z * factor);
        }
    }

    geo.computeVertexNormals();
    return geo;
}

// ═══════════════════════════════════════════════════════════════════════════
// 12 GLYPH GEOMETRY CREATORS
// ═══════════════════════════════════════════════════════════════════════════

export function createProjectGlyph() {
    const geos = [];
    const count = 4;

    for (let i = 0; i < count; i++) {
        const tetra = new THREE.TetrahedronGeometry(0.4, 0);
        const angle = (i / count) * Math.PI * 2;
        const radius = 0.3;

        const pos = tetra.attributes.position;
        for (let j = 0; j < pos.count; j++) {
            const x = pos.getX(j);
            const y = pos.getY(j);
            const z = pos.getZ(j);
            pos.setXYZ(j,
                x + Math.cos(angle) * radius,
                y + Math.sin(angle) * radius * 0.5,
                z + Math.sin(angle) * radius
            );
        }

        geos.push(tetra);
    }

    return mergeGeometries(geos);
}

export function createDecisionGlyph() {
    const geos = [];

    const bar = new THREE.BoxGeometry(1.0, 0.08, 0.08);
    geos.push(bar);

    const pillar = new THREE.CylinderGeometry(0.05, 0.05, 0.6, 8);
    pillar.translate(0, -0.3, 0);
    geos.push(pillar);

    const leftPan = new THREE.SphereGeometry(0.2, 12, 12);
    leftPan.translate(-0.4, -0.2, 0);
    geos.push(leftPan);

    const rightPan = new THREE.SphereGeometry(0.2, 12, 12);
    rightPan.translate(0.4, -0.2, 0);
    geos.push(rightPan);

    return mergeGeometries(geos);
}

export function createFactGlyph() {
    return createBeveledBox(0.7, 0.08);
}

export function createProblemGlyph() {
    const geo = new THREE.IcosahedronGeometry(0.35, 0);
    const pos = geo.attributes.position;

    for (let i = 0; i < pos.count; i += 2) {
        const x = pos.getX(i);
        const y = pos.getY(i);
        const z = pos.getZ(i);
        const len = Math.sqrt(x*x + y*y + z*z);
        const factor = 1.8;
        pos.setXYZ(i, x / len * factor, y / len * factor, z / len * factor);
    }

    geo.computeVertexNormals();
    return geo;
}

export function createPersonGlyph() {
    const geos = [];

    const head = new THREE.SphereGeometry(0.35, 16, 16, 0, Math.PI * 2, 0, Math.PI / 2);
    head.translate(0, 0.15, 0);
    geos.push(head);

    const neck = new THREE.CylinderGeometry(0.12, 0.15, 0.25, 8);
    neck.translate(0, -0.15, 0);
    geos.push(neck);

    const shoulders = new THREE.CylinderGeometry(0.05, 0.35, 0.2, 8);
    shoulders.translate(0, -0.35, 0);
    geos.push(shoulders);

    return mergeGeometries(geos);
}

export function createSessionGlyph() {
    const geos = [];

    const center = new THREE.SphereGeometry(0.1, 8, 8);
    geos.push(center);

    const ring1 = new THREE.TorusGeometry(0.25, 0.03, 6, 16);
    ring1.rotateX(Math.PI / 3);
    geos.push(ring1);

    const ring2 = new THREE.TorusGeometry(0.25, 0.03, 6, 16);
    ring2.rotateY(Math.PI / 3);
    geos.push(ring2);

    return mergeGeometries(geos);
}

export function createEventGlyph() {
    const geos = [];
    const count = 5;

    for (let i = 0; i < count; i++) {
        const cone = new THREE.ConeGeometry(0.08, 0.5, 6);
        const angle = (i / count) * Math.PI * 2;
        cone.rotateX(Math.PI / 2);
        cone.rotateY(angle);
        cone.translate(
            Math.cos(angle) * 0.1,
            0.15,
            Math.sin(angle) * 0.1
        );
        geos.push(cone);
    }

    return mergeGeometries(geos);
}

export function createAgentGlyph() {
    const geos = [];

    const eye = new THREE.SphereGeometry(0.35, 16, 16);
    geos.push(eye);

    const iris = new THREE.TorusGeometry(0.18, 0.04, 8, 16);
    iris.rotateX(Math.PI / 2);
    iris.translate(0, 0, 0.28);
    geos.push(iris);

    const pupil = new THREE.SphereGeometry(0.08, 8, 8);
    pupil.translate(0, 0, 0.32);
    geos.push(pupil);

    return mergeGeometries(geos);
}

export function createConceptGlyph() {
    const geos = [];

    const ring1 = new THREE.TorusGeometry(0.25, 0.05, 8, 20);
    geos.push(ring1);

    const ring2 = new THREE.TorusGeometry(0.25, 0.05, 8, 20);
    ring2.rotateX(Math.PI / 2);
    geos.push(ring2);

    const ring3 = new THREE.TorusGeometry(0.25, 0.05, 8, 20);
    ring3.rotateY(Math.PI / 2);
    geos.push(ring3);

    return mergeGeometries(geos);
}

export function createTopicGlyph() {
    const geos = [];

    const leftPage = new THREE.BoxGeometry(0.4, 0.5, 0.03);
    leftPage.rotateZ(0.2);
    leftPage.translate(-0.22, 0, 0);
    geos.push(leftPage);

    const rightPage = new THREE.BoxGeometry(0.4, 0.5, 0.03);
    rightPage.rotateZ(-0.2);
    rightPage.translate(0.22, 0, 0);
    geos.push(rightPage);

    const spine = new THREE.CylinderGeometry(0.04, 0.04, 0.5, 8);
    spine.rotateX(Math.PI / 2);
    geos.push(spine);

    return mergeGeometries(geos);
}

export function createPreferenceGlyph() {
    const geos = [];

    const leftLobe = new THREE.SphereGeometry(0.22, 12, 12);
    leftLobe.translate(-0.15, 0.15, 0);
    geos.push(leftLobe);

    const rightLobe = new THREE.SphereGeometry(0.22, 12, 12);
    rightLobe.translate(0.15, 0.15, 0);
    geos.push(rightLobe);

    const point = new THREE.ConeGeometry(0.25, 0.4, 8);
    point.rotateZ(Math.PI);
    point.translate(0, -0.15, 0);
    geos.push(point);

    return mergeGeometries(geos);
}

export function createQuestionGlyph() {
    const geos = [];

    const curve = new THREE.TorusGeometry(0.2, 0.06, 8, 16, Math.PI * 1.3);
    curve.rotateZ(Math.PI / 2);
    curve.translate(0, 0.15, 0);
    geos.push(curve);

    const stem = new THREE.CylinderGeometry(0.06, 0.06, 0.25, 8);
    stem.translate(0, -0.15, 0);
    geos.push(stem);

    const dot = new THREE.SphereGeometry(0.08, 8, 8);
    dot.translate(0, -0.35, 0);
    geos.push(dot);

    return mergeGeometries(geos);
}

// ═══════════════════════════════════════════════════════════════════════════
// GLYPH REGISTRY
// ═══════════════════════════════════════════════════════════════════════════

export const GLYPH_CREATORS = {
    project: createProjectGlyph,
    decision: createDecisionGlyph,
    fact: createFactGlyph,
    problem: createProblemGlyph,
    person: createPersonGlyph,
    session: createSessionGlyph,
    event: createEventGlyph,
    agent: createAgentGlyph,
    concept: createConceptGlyph,
    topic: createTopicGlyph,
    preference: createPreferenceGlyph,
    question: createQuestionGlyph,
};

// LOD fallback: simple cube for distant nodes
export function createLODGlyph() {
    return new THREE.BoxGeometry(0.5, 0.5, 0.5);
}
