-- Migration 062: Add covering index for snapshot query and edge count limits
-- Fixes BUG-P025 (snapshot query performance) and supports BUG-P047/P048 (count limits)

-- Covering index for snapshot query (workspace + valid_to + importance + access_count)
-- This helps the ORDER BY score in snapshot generation
CREATE INDEX IF NOT EXISTS idx_nodes_snapshot_covering 
ON nodes (workspace_name, valid_to, importance DESC, access_count DESC)
WHERE valid_to IS NULL;

-- Index for fast node count per workspace (supports BUG-P047)
CREATE INDEX IF NOT EXISTS idx_nodes_workspace_count 
ON nodes (workspace_name, node_type) 
WHERE valid_to IS NULL;

-- Index for fast edge count per node (supports BUG-P048)
CREATE INDEX IF NOT EXISTS idx_edges_source_count 
ON edges (source_id, edge_type);

CREATE INDEX IF NOT EXISTS idx_edges_target_count 
ON edges (target_id, edge_type);
