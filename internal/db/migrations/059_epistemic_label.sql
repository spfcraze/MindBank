-- 059: Add epistemic_label column to nodes
-- Tracks the epistemic status of a node (unknown, fact, belief, assumption, etc.)

ALTER TABLE nodes ADD COLUMN IF NOT EXISTS epistemic_label VARCHAR(20) DEFAULT 'unknown';

-- Index for fast filtering by epistemic status
CREATE INDEX IF NOT EXISTS idx_nodes_epistemic_label ON nodes(epistemic_label)
    WHERE valid_to IS NULL;
