-- 013: AgentMemory features
-- 1. node_hashes table for deduplication
CREATE TABLE IF NOT EXISTS node_hashes (
    hash        TEXT PRIMARY KEY,
    node_id     TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    created_at  TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_node_hashes_node_id ON node_hashes(node_id);

-- 2. Add compress flag to embedding_queue
ALTER TABLE embedding_queue
    ADD COLUMN IF NOT EXISTS compress BOOLEAN NOT NULL DEFAULT false;

-- 3. session_messages table for per-session message storage
CREATE TABLE IF NOT EXISTS session_messages (
    id          BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    session_id  TEXT NOT NULL REFERENCES sessions(id) ON DELETE CASCADE,
    role        TEXT NOT NULL CHECK (role IN ('user', 'assistant', 'system', 'tool')),
    content     TEXT NOT NULL DEFAULT '',
    timestamp   TIMESTAMPTZ NOT NULL DEFAULT now(),
    metadata    JSONB NOT NULL DEFAULT '{}'
);

CREATE INDEX IF NOT EXISTS idx_session_messages_session ON session_messages(session_id, timestamp DESC);
CREATE INDEX IF NOT EXISTS idx_session_messages_metadata ON session_messages USING GIN(metadata jsonb_path_ops);

-- 4. captured_sessions table for file-based session capture
CREATE TABLE IF NOT EXISTS captured_sessions (
    id              TEXT PRIMARY KEY DEFAULT gen_random_uuid()::text,
    file_path       TEXT NOT NULL,
    file_hash       TEXT NOT NULL,
    node_id         TEXT REFERENCES nodes(id) ON DELETE SET NULL,
    status          TEXT NOT NULL DEFAULT 'pending' CHECK (status IN ('pending', 'processing', 'done', 'failed')),
    error           TEXT,
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at    TIMESTAMPTZ
);

CREATE INDEX IF NOT EXISTS idx_captured_sessions_status ON captured_sessions(status, created_at);
CREATE INDEX IF NOT EXISTS idx_captured_sessions_node ON captured_sessions(node_id);
CREATE INDEX IF NOT EXISTS idx_captured_sessions_hash ON captured_sessions(file_hash);
