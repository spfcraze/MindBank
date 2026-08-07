-- Settings table for user-configurable options
CREATE TABLE IF NOT EXISTS settings (
    key TEXT PRIMARY KEY,
    value TEXT NOT NULL,
    updated_at TIMESTAMPTZ NOT NULL DEFAULT now()
);

-- Default compression settings (disabled by default)
INSERT INTO settings (key, value) VALUES
    ('compression_enabled', 'false'),
    ('compression_model', 'phi4-mini'),
    ('compression_setup_complete', 'false')
ON CONFLICT (key) DO NOTHING;
