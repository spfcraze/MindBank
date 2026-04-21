-- 056_materialized_path.sql
-- Add materialized_path column for O(1) ancestor/descendant lookups
-- Path format: /{id1}/{id2}/{id3} where id1 is root, id3 is leaf

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS materialized_path TEXT DEFAULT '/';

-- Index for fast ancestor/descendant queries
CREATE INDEX IF NOT EXISTS idx_nodes_materialized_path ON nodes (materialized_path text_pattern_ops);

-- Backfill: set path to /{id} for all existing nodes
UPDATE nodes SET materialized_path = '/' || id WHERE materialized_path = '/' OR materialized_path = '';

-- Backfill: build paths from existing edges
-- For each edge A -> B, set B's path to A's path + B's id
WITH RECURSIVE path_builder AS (
    -- Root nodes: no incoming edges (or path already set)
    SELECT id, materialized_path, 0 as depth
    FROM nodes
    WHERE valid_to IS NULL
      AND materialized_path IS NOT NULL
      AND materialized_path != '/'

    UNION ALL

    -- Children: follow edges to build paths
    SELECT n.id,
           CASE
               WHEN pb.materialized_path = '/' || pb.id THEN '/' || pb.id || '/' || n.id
               ELSE pb.materialized_path || '/' || n.id
           END as materialized_path,
           pb.depth + 1
    FROM nodes n
    JOIN edges e ON e.target_id = n.id AND e.valid_to IS NULL
    JOIN path_builder pb ON pb.id = e.source_id
    WHERE n.valid_to IS NULL
      AND pb.depth < 10  -- prevent infinite loops
)
UPDATE nodes
SET materialized_path = pb.materialized_path
FROM path_builder pb
WHERE nodes.id = pb.id
  AND pb.depth > 0;
