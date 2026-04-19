-- 056_materialized_path.sql
-- Add materialized_path column for O(1) ancestor/descendant lookups

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS materialized_path TEXT DEFAULT '/';
CREATE INDEX IF NOT EXISTS idx_nodes_materialized_path ON nodes (materialized_path text_pattern_ops);
UPDATE nodes SET materialized_path = '/' || id WHERE materialized_path = '/' OR materialized_path = '';
