-- 004: Edges (mindmap connections)
DO $$ BEGIN
    CREATE TYPE edge_type AS ENUM (
        'contains', 'relates_to', 'depends_on', 'decided_by',
        'participated_in', 'produced', 'contradicts', 'supports',
        'temporal_next', 'mentions', 'learned_from'
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS edges (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_name  TEXT NOT NULL REFERENCES workspaces(name),
    source_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    target_id       TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    edge_type       edge_type NOT NULL,
    weight          REAL NOT NULL DEFAULT 1.0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(source_id, target_id, edge_type)
);

CREATE INDEX IF NOT EXISTS idx_edges_workspace ON edges(workspace_name);
CREATE INDEX IF NOT EXISTS idx_edges_source ON edges(source_id);
CREATE INDEX IF NOT EXISTS idx_edges_target ON edges(target_id);
CREATE INDEX IF NOT EXISTS idx_edges_type ON edges(edge_type);
CREATE INDEX IF NOT EXISTS idx_edges_weight ON edges(weight DESC);
CREATE INDEX IF NOT EXISTS idx_edges_source_type ON edges(source_id, edge_type);
CREATE INDEX IF NOT EXISTS idx_edges_target_type ON edges(target_id, edge_type);
