-- 070: Workspace events — auditable capture of every J-space transition.
-- The global workspace is a leasing, event-driven layer: memories enter
-- (created/reinforced), get leased (renewed while lit up), are consumed
-- (acted upon to completion), or expire. Recording these transitions lets
-- the JSPACE tab capture the workspace as a stream, not a poll.
CREATE TABLE IF NOT EXISTS workspace_events (
    id         BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    event_type TEXT NOT NULL CHECK (event_type IN
                 ('created', 'reinforced', 'entered', 'left', 'consumed', 'expired')),
    node_id    TEXT,
    label      TEXT,
    namespace  TEXT,
    meta       JSONB NOT NULL DEFAULT '{}',
    created_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_workspace_events_created ON workspace_events(created_at DESC);
