-- Migration 061: Add foreign key constraints for referential integrity
-- This fixes BUG-P013: Zero foreign key constraints

-- Note: Adding FKs to existing tables requires careful ordering.
-- We add them as DEFERRABLE INITIALLY DEFERRED to avoid issues during batch operations.

-- FK on edges.source_id -> nodes.id
-- Must clean up orphaned edges first
DELETE FROM edges 
WHERE source_id NOT IN (SELECT id FROM nodes WHERE valid_to IS NULL)
   OR target_id NOT IN (SELECT id FROM nodes WHERE valid_to IS NULL);

ALTER TABLE edges
ADD CONSTRAINT fk_edges_source 
    FOREIGN KEY (source_id) REFERENCES nodes(id) ON DELETE CASCADE;

ALTER TABLE edges
ADD CONSTRAINT fk_edges_target 
    FOREIGN KEY (target_id) REFERENCES nodes(id) ON DELETE CASCADE;

-- FK on node_embeddings.node_id -> nodes.id
-- Clean up orphaned embeddings first
DELETE FROM node_embeddings 
WHERE node_id NOT IN (SELECT id FROM nodes WHERE valid_to IS NULL);

ALTER TABLE node_embeddings
ADD CONSTRAINT fk_embeddings_node 
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE;

-- FK on session_nodes.node_id -> nodes.id
-- Clean up orphaned associations first
DELETE FROM session_nodes 
WHERE node_id NOT IN (SELECT id FROM nodes WHERE valid_to IS NULL);

ALTER TABLE session_nodes
ADD CONSTRAINT fk_session_node 
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE;

-- FK on collection_nodes.node_id -> nodes.id
-- Clean up orphaned associations first
DELETE FROM collection_nodes 
WHERE node_id NOT IN (SELECT id FROM nodes WHERE valid_to IS NULL);

ALTER TABLE collection_nodes
ADD CONSTRAINT fk_collection_node 
    FOREIGN KEY (node_id) REFERENCES nodes(id) ON DELETE CASCADE;

-- FK on nodes.predecessor_id -> nodes.id (self-referential, nullable)
-- Must handle NULLs and existing invalid references
UPDATE nodes SET predecessor_id = NULL 
WHERE predecessor_id IS NOT NULL 
  AND predecessor_id NOT IN (SELECT id FROM nodes);

ALTER TABLE nodes
ADD CONSTRAINT fk_predecessor 
    FOREIGN KEY (predecessor_id) REFERENCES nodes(id) ON DELETE SET NULL;

-- Add indexes to support FK lookups (if not already present)
CREATE INDEX IF NOT EXISTS idx_edges_source_id ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target_id ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_node_embeddings_node_id ON node_embeddings(node_id);
CREATE INDEX IF NOT EXISTS idx_session_nodes_node_id ON session_nodes(node_id);
CREATE INDEX IF NOT EXISTS idx_collection_nodes_node_id ON collection_nodes(node_id);
CREATE INDEX IF NOT EXISTS idx_nodes_predecessor_id ON nodes(predecessor_id) WHERE predecessor_id IS NOT NULL;
