-- 069: Taxonomy — auto-classification topics for nodes
-- Adds topic classification support without breaking existing data

-- Add topic column with safe fallback for existing databases
DO $$ BEGIN
    ALTER TABLE nodes ADD COLUMN topic TEXT DEFAULT '';
EXCEPTION WHEN duplicate_column THEN NULL;
END $$;

-- Add auto_classified flag to track which nodes were machine-tagged
DO $$ BEGIN
    ALTER TABLE nodes ADD COLUMN auto_classified BOOLEAN DEFAULT FALSE;
EXCEPTION WHEN duplicate_column THEN NULL;
END $$;

-- Index for fast topic filtering
CREATE INDEX IF NOT EXISTS idx_nodes_topic ON nodes(topic) WHERE valid_to IS NULL;

-- Index for finding unclassified nodes
CREATE INDEX IF NOT EXISTS idx_nodes_unclassified ON nodes(workspace_name, node_type) 
    WHERE valid_to IS NULL AND (topic = '' OR topic IS NULL);
