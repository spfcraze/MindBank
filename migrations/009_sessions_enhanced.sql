-- 009: Enhance sessions table for session storage
-- Adds columns needed to store full session data from Hermes sessions

-- Add session_data JSONB column for full session content
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS session_data JSONB NOT NULL DEFAULT '{}';

-- Add namespace column for project filtering
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS namespace TEXT NOT NULL DEFAULT 'global';

-- Add source_path column to track original file location
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS source_path TEXT NOT NULL DEFAULT '';

-- Add message_count for quick stats
ALTER TABLE sessions ADD COLUMN IF NOT EXISTS message_count INTEGER NOT NULL DEFAULT 0;

-- Create index for namespace filtering
CREATE INDEX IF NOT EXISTS idx_sessions_namespace ON sessions(namespace);

-- Create index for source_path deduplication
CREATE INDEX IF NOT EXISTS idx_sessions_source_path ON sessions(source_path);
