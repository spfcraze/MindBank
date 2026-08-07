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
