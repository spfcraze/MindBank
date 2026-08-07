-- 003: Nodes (mindmap vertices with temporal fields)
DO $$ BEGIN
    CREATE TYPE node_type AS ENUM (
        'person', 'agent', 'project', 'topic', 'decision',
        'fact', 'event', 'preference', 'advice', 'problem', 'concept', 'question'
    );
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

CREATE TABLE IF NOT EXISTS nodes (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_name  TEXT NOT NULL REFERENCES workspaces(name),
    namespace       TEXT NOT NULL DEFAULT 'global',
    label           TEXT NOT NULL,
    node_type       node_type NOT NULL,
    content         TEXT NOT NULL DEFAULT '',
    summary         TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    importance      REAL NOT NULL DEFAULT 0.5,
    access_count    INTEGER NOT NULL DEFAULT 0,
    last_accessed   TIMESTAMPTZ,
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to        TIMESTAMPTZ,
    version         INTEGER NOT NULL DEFAULT 1,
    predecessor_id  TEXT REFERENCES nodes(id),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    -- Full text search
    search_vector   tsvector GENERATED ALWAYS AS (
        to_tsvector('english', coalesce(label, '') || ' ' || coalesce(content, '') || ' ' || coalesce(summary, ''))
    ) STORED,

    CONSTRAINT node_label_len CHECK (length(label) <= 512)
);

CREATE INDEX IF NOT EXISTS idx_nodes_workspace ON nodes(workspace_name);
CREATE INDEX IF NOT EXISTS idx_nodes_namespace ON nodes(namespace);
CREATE INDEX IF NOT EXISTS idx_nodes_type ON nodes(node_type);
CREATE INDEX IF NOT EXISTS idx_nodes_label ON nodes(label);
CREATE INDEX IF NOT EXISTS idx_nodes_importance ON nodes(importance DESC);
CREATE INDEX IF NOT EXISTS idx_nodes_search ON nodes USING GIN(search_vector);
CREATE INDEX IF NOT EXISTS idx_nodes_metadata ON nodes USING GIN(metadata jsonb_path_ops);
CREATE INDEX IF NOT EXISTS idx_nodes_updated ON nodes(updated_at DESC);

-- Partial indexes for current-state queries (fast)
CREATE INDEX IF NOT EXISTS idx_nodes_current ON nodes(workspace_name, namespace, node_type)
    WHERE valid_to IS NULL;

CREATE INDEX IF NOT EXISTS idx_nodes_label_current ON nodes(workspace_name, label, node_type)
    WHERE valid_to IS NULL;

-- Version chain traversal
CREATE INDEX IF NOT EXISTS idx_nodes_predecessor ON nodes(predecessor_id)
    WHERE predecessor_id IS NOT NULL;

-- Temporal range queries
CREATE INDEX IF NOT EXISTS idx_nodes_temporal ON nodes(valid_from, valid_to);
