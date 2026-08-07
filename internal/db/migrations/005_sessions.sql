-- 005: Sessions
CREATE TABLE IF NOT EXISTS sessions (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_name  TEXT NOT NULL REFERENCES workspaces(name),
    name            TEXT NOT NULL DEFAULT '',
    is_active       BOOLEAN NOT NULL DEFAULT true,
    metadata        JSONB NOT NULL DEFAULT '{}',
    summary         TEXT NOT NULL DEFAULT '',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_sessions_workspace ON sessions(workspace_name);
CREATE INDEX IF NOT EXISTS idx_sessions_active ON sessions(is_active) WHERE is_active = true;
CREATE INDEX IF NOT EXISTS idx_sessions_created ON sessions(created_at DESC);

-- Many-to-many: which nodes are referenced in which sessions
CREATE TABLE IF NOT EXISTS session_nodes (
    session_id      TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    mention_count   INTEGER NOT NULL DEFAULT 1,
    first_mentioned TIMESTAMPTZ NOT NULL DEFAULT now(),
    last_mentioned  TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (session_id, node_id)
);
