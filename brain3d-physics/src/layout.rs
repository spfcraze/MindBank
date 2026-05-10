use crate::types::Node;

pub fn init_positions(nodes: &mut [Node]) {
    let n = nodes.len() as f32;
    let radius = (n * 2.0).sqrt();

    for (i, node) in nodes.iter_mut().enumerate() {
        let t = i as f32 / n.max(1.0);
        let angle = t * 2.0 * std::f32::consts::PI * 17.0;
        let y = 1.0 - (t * 2.0);
        let r = (1.0 - y * y).sqrt() * radius;

        node.pos[0] = r * angle.cos();
        node.pos[1] = y * radius;
        node.pos[2] = r * angle.sin();
        node.vel = [0.0, 0.0, 0.0];
    }
}

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
