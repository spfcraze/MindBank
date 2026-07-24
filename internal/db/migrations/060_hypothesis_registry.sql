-- 060: Add hypothesis registry fields to nodes
-- Status tracking, confirmation count, source attribution, and blocker notes
-- for epistemic validation lifecycle (open → supported → refuted → inconclusive → blocked)

ALTER TABLE nodes
    ADD COLUMN IF NOT EXISTS status VARCHAR(20) DEFAULT 'open'
        CHECK (status IN ('open', 'supported', 'refuted', 'inconclusive', 'blocked')),
    ADD COLUMN IF NOT EXISTS confirmation_count INTEGER NOT NULL DEFAULT 0,
    ADD COLUMN IF NOT EXISTS source TEXT DEFAULT '',
    ADD COLUMN IF NOT EXISTS blocker TEXT DEFAULT '';

-- Index for fast filtering by validation status (current nodes only)
CREATE INDEX IF NOT EXISTS idx_nodes_status ON nodes(status)
    WHERE valid_to IS NULL;

-- Index for confirmation-based ranking (current nodes only)
CREATE INDEX IF NOT EXISTS idx_nodes_confirmation ON nodes(confirmation_count DESC)
    WHERE valid_to IS NULL;

-- Composite index for snapshot queries: status + confirmation + importance
CREATE INDEX IF NOT EXISTS idx_nodes_snapshot_rank ON nodes(status, confirmation_count DESC, importance DESC)
    WHERE valid_to IS NULL;

-- Index for source-based lookups (e.g., "find all memories from session X")
CREATE INDEX IF NOT EXISTS idx_nodes_source ON nodes(source)
    WHERE valid_to IS NULL AND source <> '';
