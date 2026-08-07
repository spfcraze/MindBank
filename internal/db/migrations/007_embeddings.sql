-- 007: Embeddings (pgvector 768 dims — nomic-embed-text)

-- Node embeddings
CREATE TABLE IF NOT EXISTS node_embeddings (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    node_id         TEXT NOT NULL REFERENCES nodes(id) ON DELETE CASCADE,
    content         TEXT NOT NULL,
    embedding       vector(768),
    sync_state      TEXT NOT NULL DEFAULT 'pending' CHECK (sync_state IN ('pending', 'synced', 'failed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(node_id)
);

CREATE INDEX IF NOT EXISTS idx_node_embeddings_vec ON node_embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX IF NOT EXISTS idx_node_embeddings_sync ON node_embeddings(sync_state) WHERE sync_state = 'pending';

-- Message embeddings
CREATE TABLE IF NOT EXISTS message_embeddings (
    id              BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    message_id      BIGINT NOT NULL REFERENCES messages(id) ON DELETE CASCADE,
    content         TEXT NOT NULL,
    embedding       vector(768),
    sync_state      TEXT NOT NULL DEFAULT 'pending' CHECK (sync_state IN ('pending', 'synced', 'failed')),
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),

    UNIQUE(message_id)
);

CREATE INDEX IF NOT EXISTS idx_message_embeddings_vec ON message_embeddings
    USING hnsw (embedding vector_cosine_ops)
    WITH (m = 16, ef_construction = 64);

CREATE INDEX IF NOT EXISTS idx_message_embeddings_sync ON message_embeddings(sync_state) WHERE sync_state = 'pending';
