-- 067: Dedicated queue for the compression worker.
-- The worker previously targeted embedding_queue columns that never existed
-- (node_id/error/completed_at, status 'completed'), so it errored on every
-- cycle; sharing embedding_queue would also let compression status updates
-- clobber the embed worker's rows.
CREATE TABLE IF NOT EXISTS compression_queue (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id      TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    status       TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts     INTEGER NOT NULL DEFAULT 0,
    last_error   TEXT,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at TIMESTAMPTZ,

    UNIQUE(node_id)
);

CREATE INDEX IF NOT EXISTS idx_compression_queue_status ON compression_queue(status, created_at);
