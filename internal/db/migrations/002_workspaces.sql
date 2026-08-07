-- 002: Workspaces
CREATE TABLE IF NOT EXISTS workspaces (
    id          TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    name        TEXT NOT NULL UNIQUE,
    metadata    JSONB NOT NULL DEFAULT '{}',
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at  TIMESTAMPTZ NOT NULL DEFAULT now(),

    CONSTRAINT workspace_name_len CHECK (length(name) <= 255)
);

CREATE INDEX IF NOT EXISTS idx_workspaces_name ON workspaces(name);

-- Default workspace
INSERT INTO workspaces (name) VALUES ('hermes')
ON CONFLICT (name) DO NOTHING;
