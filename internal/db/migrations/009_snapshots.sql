-- 009: Snapshots (pre-computed wake-up context)
CREATE TABLE IF NOT EXISTS snapshots (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    workspace_name  TEXT NOT NULL REFERENCES workspaces(name),
    name            TEXT NOT NULL DEFAULT 'default',
    content         TEXT NOT NULL,
    token_count     INTEGER NOT NULL DEFAULT 0,
    node_count      INTEGER NOT NULL DEFAULT 0,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(workspace_name, name)
);
