-- 058: User Profiles — structured fact extraction
-- Stores extracted user facts with category, confidence, and source link

CREATE TABLE IF NOT EXISTS profiles (
    id              UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    
    category        TEXT NOT NULL CHECK (category IN ('preference', 'fact', 'goal', 'project', 'skill', 'constraint')),
    fact            TEXT NOT NULL,
    confidence      REAL NOT NULL DEFAULT 0.5 CHECK (confidence >= 0 AND confidence <= 1),
    source_node_id  UUID REFERENCES nodes(id) ON DELETE SET NULL,
    valid_from      TIMESTAMPTZ NOT NULL DEFAULT now(),
    valid_to        TIMESTAMPTZ,
    metadata        JSONB NOT NULL DEFAULT '{}',
    created_at      TIMESTAMPTZ NOT NULL DEFAULT now(),
    updated_at      TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_profiles_category ON profiles(category);
CREATE INDEX IF NOT EXISTS idx_profiles_current ON profiles(category, fact)
    WHERE valid_to IS NULL;
CREATE INDEX IF NOT EXISTS idx_profiles_confidence ON profiles(confidence DESC)
    WHERE valid_to IS NULL;
CREATE INDEX IF NOT EXISTS idx_profiles_source ON profiles(source_node_id);
