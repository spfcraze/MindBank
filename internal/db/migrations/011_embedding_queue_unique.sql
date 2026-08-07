-- 011: Fix embedding_queue — add unique constraint for ON CONFLICT DO NOTHING
ALTER TABLE embedding_queue
    ADD CONSTRAINT uq_embedding_queue_source UNIQUE (source_type, source_id);
