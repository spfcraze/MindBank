-- 057: Auto Forgetting — TTL support for nodes
-- Adds expires_at column, expiry index, and forgetting log table

-- 1. Add expires_at column to nodes (nullable = never expires)
ALTER TABLE nodes
    ADD COLUMN IF NOT EXISTS expires_at TIMESTAMPTZ;

-- 2. Add index for efficient expiry queries
CREATE INDEX IF NOT EXISTS idx_nodes_expires_at 
    ON nodes(expires_at) 
    WHERE expires_at IS NOT NULL AND valid_to IS NULL;

-- 3. Add forgetting_log table for audit trail
CREATE TABLE IF NOT EXISTS forgetting_log (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id     TEXT NOT NULL REFERENCES nodes(id),
    action      TEXT NOT NULL CHECK (action IN ('expired', 'pinned', 'unpinned', 'manual_superseded')),
    reason      TEXT,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_forgetting_log_node ON forgetting_log(node_id);
CREATE INDEX IF NOT EXISTS idx_forgetting_log_created ON forgetting_log(created_at DESC);
