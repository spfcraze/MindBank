-- 010: Embedding queue
CREATE TABLE IF NOT EXISTS embedding_queue (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    source_type     TEXT NOT NULL CHECK (source_type IN ('node', 'message')),
    source_id       TEXT NOT NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    attempts        INTEGER NOT NULL DEFAULT 0,
    last_error      TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    processed_at    TIMESTAMPTZ,

    UNIQUE(source_type, source_id)
);

CREATE INDEX IF NOT EXISTS idx_embedding_queue_status ON embedding_queue(status, created_at);
