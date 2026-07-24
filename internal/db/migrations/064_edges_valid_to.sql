-- Migration 064: Add valid_to to edges table for temporal versioning
-- The dream engine (neural consolidation) requires soft-delete capability on edges

-- Add valid_to column to edges (if not exists)
DO $$
BEGIN
    IF NOT EXISTS (
        SELECT 1 FROM information_schema.columns
        WHERE table_name = 'edges' AND column_name = 'valid_to'
    ) THEN
        ALTER TABLE edges ADD COLUMN valid_to TIMESTAMPTZ;
    END IF;
END $$;

-- Create index for valid_to queries
CREATE INDEX IF NOT EXISTS idx_edges_valid_to ON edges(valid_to) WHERE valid_to IS NULL;

-- Add partial index for active edges (valid_to IS NULL)
CREATE INDEX IF NOT EXISTS idx_edges_active ON edges(source_id, target_id, edge_type) WHERE valid_to IS NULL;
