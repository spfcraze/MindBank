-- Migration 065: Conflict Detection, Token Reranking, Graph-Aware Embeddings
-- Adds tables for MindBank's advanced neural consolidation features

-- 1. Conflict Registry
-- Tracks detected contradictions between nodes
CREATE TABLE IF NOT EXISTS conflicts (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_a_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    node_b_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    similarity      REAL NOT NULL,           -- cosine similarity (should be >= 0.85)
    node_a_value    JSONB NOT NULL,          -- conflicting attribute from node A
    node_b_value    JSONB NOT NULL,          -- conflicting attribute from node B
    attribute_path  TEXT NOT NULL,           -- e.g., "status", "confidence", "category"
    resolution      TEXT,                    -- 'superseded', 'merged', 'open', 'false_positive'
    winner_id       TEXT REFERENCES nodes(id), -- which node won (if resolved)
    detected_at     TIMESTAMPTZ NOT NULL DEFAULT now(),
    resolved_at     TIMESTAMPTZ,
    detected_by     TEXT NOT NULL DEFAULT 'conflict_engine', -- engine version
    
    UNIQUE(node_a_id, node_b_id, attribute_path)
);

CREATE INDEX IF NOT EXISTS idx_conflicts_node_a ON conflicts(node_a_id);
CREATE INDEX IF NOT EXISTS idx_conflicts_node_b ON conflicts(node_b_id);
CREATE INDEX IF NOT EXISTS idx_conflicts_resolution ON conflicts(resolution) WHERE resolution IS NULL;
CREATE INDEX IF NOT EXISTS idx_conflicts_detected ON conflicts(detected_at DESC);

-- 2. Token Cache (ColBERT equivalent)
-- Stores per-token embeddings for late-interaction reranking
CREATE TABLE IF NOT EXISTS token_cache (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    tokens          TEXT[] NOT NULL,         -- tokenized text
    token_embeddings vector(768)[],          -- per-token embeddings (768-dim each)
    model_version   TEXT NOT NULL DEFAULT 'nomic-embed-text-v1',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE(node_id, model_version)
);

CREATE INDEX IF NOT EXISTS idx_token_cache_node ON token_cache(node_id);

-- 3. Graph-Aware Embeddings (DAE equivalent)
-- Embeddings that incorporate graph structure (neighbor context)
CREATE TABLE IF NOT EXISTS graph_embeddings (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    embedding       vector(768),             -- graph-aware embedding
    neighbor_ids    TEXT[],                  -- IDs of neighbors used in context
    hop_count       INTEGER NOT NULL DEFAULT 1, -- how many hops of context included
    model_version   TEXT NOT NULL DEFAULT 'graph-nomic-v1',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    
    UNIQUE(node_id, hop_count, model_version)
);

CREATE INDEX IF NOT EXISTS idx_graph_embeddings_node ON graph_embeddings(node_id);
CREATE INDEX IF NOT EXISTS idx_graph_embeddings_vec ON graph_embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

-- 4. Memory Revisions (already created in 063 with superseded_id/superseding_id)
-- No need to recreate - the 063 schema is sufficient for conflict tracking
