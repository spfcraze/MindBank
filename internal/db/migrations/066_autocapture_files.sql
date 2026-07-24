-- 066: Durable ledger of autocapture file ingestion.
-- Keyed on (path, content_hash): a truncated file ingested mid-write gets a
-- different hash than the completed file, so the finished session is still
-- picked up, while restarts no longer re-ingest everything in the watch dir.
CREATE TABLE IF NOT EXISTS autocapture_files (
    path         TEXT NOT NULL,
    content_hash TEXT NOT NULL,
    outcome      TEXT NOT NULL DEFAULT 'ingested',
    session_id   TEXT,
    processed_at TIMESTAMPTZ NOT NULL DEFAULT now(),
    PRIMARY KEY (path, content_hash)
);

CREATE INDEX IF NOT EXISTS idx_autocapture_files_processed ON autocapture_files(processed_at);
