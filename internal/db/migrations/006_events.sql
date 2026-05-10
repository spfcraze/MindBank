-- Migration 006: Event nodes and temporal edges
-- Adds event node type and temporal_next edge type

-- Add event to node_type enum
ALTER TYPE node_type ADD VALUE IF NOT EXISTS 'event';

-- Add edge_type for temporal linking
ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'temporal_next';

-- Index for fast session event lookup
CREATE INDEX IF NOT EXISTS idx_nodes_session_events 
ON nodes(namespace, node_type) 
WHERE node_type = 'event';

-- Index for temporal edge traversal
CREATE INDEX IF NOT EXISTS idx_edges_temporal 
ON edges(edge_type) 
WHERE edge_type = 'temporal_next';
