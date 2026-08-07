-- Migration 063: Dream Engine — Neural Consolidation
-- Adds columns for MindBank's sleep-inspired graph maintenance system

-- 1. Add token_cache for neural reranking (stores per-token embeddings for precision recall)
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS token_cache BYTEA;

-- 2. Add neural_embedding for graph-aware vectors (neighbor-weighted embeddings)
ALTER TABLE nodes ADD COLUMN IF NOT EXISTS neural_embedding vector(768);

-- 3. Add memory_revisions table for conflict tracking
CREATE TABLE IF NOT EXISTS memory_revisions (
    id SERIAL PRIMARY KEY,
    superseded_id TEXT NOT NULL REFERENCES nodes(id),
    superseding_id TEXT NOT NULL REFERENCES nodes(id),
    reason TEXT NOT NULL DEFAULT 'conflict_detected',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    UNIQUE(superseded_id, superseding_id)
);

CREATE INDEX IF NOT EXISTS idx_memory_revisions_superseded ON memory_revisions(superseded_id);
CREATE INDEX IF NOT EXISTS idx_memory_revisions_superseding ON memory_revisions(superseding_id);

-- 4. Add dream_cycles table for tracking consolidation runs
CREATE TABLE IF NOT EXISTS dream_cycles (
    id SERIAL PRIMARY KEY,
    started_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at TIMESTAMPTZ,
    phase_nrem_count INT NOT NULL DEFAULT 0,
    phase_rem_count INT NOT NULL DEFAULT 0,
    phase_insight_count INT NOT NULL DEFAULT 0,
    edges_strengthened INT NOT NULL DEFAULT 0,
    edges_pruned INT NOT NULL DEFAULT 0,
    edges_bridged INT NOT NULL DEFAULT 0,
    clusters_found INT NOT NULL DEFAULT 0,
    status TEXT NOT NULL DEFAULT 'running',
    error_message TEXT
);

CREATE INDEX IF NOT EXISTS idx_dream_cycles_status ON dream_cycles(status);
CREATE INDEX IF NOT EXISTS idx_dream_cycles_started ON dream_cycles(started_at DESC);

-- 5. Add edge salience tracking (for decay/strengthen calculations)
ALTER TABLE edges ADD COLUMN IF NOT EXISTS salience REAL NOT NULL DEFAULT 1.0;
ALTER TABLE edges ADD COLUMN IF NOT EXISTS last_accessed TIMESTAMPTZ;

CREATE INDEX IF NOT EXISTS idx_edges_salience ON edges(salience DESC);
CREATE INDEX IF NOT EXISTS idx_edges_last_accessed ON edges(last_accessed);

-- 6. Add node access tracking (for three-slice sampling)
CREATE INDEX IF NOT EXISTS idx_nodes_access_count ON nodes(access_count DESC NULLS LAST);
CREATE INDEX IF NOT EXISTS idx_nodes_importance ON nodes(importance DESC NULLS LAST);

-- 7. Add derived cluster node type
-- (node_type already supports 'concept' etc; we use metadata to mark derived clusters)

-- 8. Function for random sampling (used by three-slice sampler)
CREATE OR REPLACE FUNCTION random_sample(sample_size INT)
RETURNS TABLE(id TEXT) AS $$
BEGIN
    RETURN QUERY
    SELECT n.id FROM nodes n
    WHERE n.valid_to IS NULL
    ORDER BY random()
    LIMIT sample_size;
END;
$$ LANGUAGE plpgsql;

-- 9. Function for low-salience rescue sampling
CREATE OR REPLACE FUNCTION rescue_sample(sample_size INT)
RETURNS TABLE(id TEXT) AS $$
BEGIN
    RETURN QUERY
    SELECT n.id FROM nodes n
    WHERE n.valid_to IS NULL
    ORDER BY n.importance ASC NULLS LAST, n.access_count ASC NULLS LAST
    LIMIT sample_size;
END;
$$ LANGUAGE plpgsql;
