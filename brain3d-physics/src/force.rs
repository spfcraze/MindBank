use crate::types::{Node, Edge};

const SPRING_K: f32 = 0.05;
const REPULSION_K: f32 = 800.0;
const CENTER_GRAVITY: f32 = 0.002;
const DAMPING: f32 = 0.9;
const MAX_FORCE: f32 = 10.0;

pub fn apply_forces(nodes: &mut [Node], edges: &[Edge], dt: f32, namespace_strength: f32) {
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

            // Stronger repulsion between different namespaces (tight clustering)
            let same_ns = nodes[i].namespace == nodes[j].namespace;
            let ns_mult = if same_ns { 0.3 } else { 1.0 }; // Same namespace: 30% repulsion
            let f = f * ns_mult;

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

    // Namespace gravity — pulls nodes toward their namespace centroid
    // Strength is configurable via namespace_strength parameter (default 1.0)
    let ns_gravity = 0.05 * namespace_strength; // Base 0.05, scaled by strength
    for i in 0..n {
        if let Some(center) = ns_centers.get(&nodes[i].namespace) {
            let dx = center[0] - nodes[i].pos[0];
            let dy = center[1] - nodes[i].pos[1];
            let dz = center[2] - nodes[i].pos[2];
            forces[i][0] += ns_gravity * dx;
            forces[i][1] += ns_gravity * dy;
            forces[i][2] += ns_gravity * dz;
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

        apply_forces(&mut nodes, &edges, 0.1, 1.0);

        for node in &nodes {
            assert!(!node.pos[0].is_nan());
            assert!(!node.pos[1].is_nan());
            assert!(!node.pos[2].is_nan());
        }
    }
}
