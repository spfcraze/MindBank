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
        force::apply_forces(&mut self.nodes, &self.edges, dt, self.namespace_strength);
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

    pub fn is_settled(&self) -> bool {
        let threshold = 0.01;
        self.nodes.iter().all(|n| {
            n.vel[0].abs() < threshold &&
            n.vel[1].abs() < threshold &&
            n.vel[2].abs() < threshold
        })
    }
}
