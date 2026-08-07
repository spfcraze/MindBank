-- 008: Collections (named groups of related nodes)
CREATE TABLE IF NOT EXISTS collections (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_name  TEXT NOT NULL REFERENCES workspaces(name),
    name            TEXT NOT NULL,
    description     TEXT NOT NULL DEFAULT '',
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(workspace_name, name)
);

CREATE TABLE IF NOT EXISTS collection_nodes (
    collection_id   TEXT NOT NULL REFERENCES collections(id) ON DELETE CASCADE,
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    added_at        TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (collection_id, node_id)
);
