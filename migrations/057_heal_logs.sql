-- 057: Heal logs (audit trail for debug self-healing)
CREATE TABLE IF NOT EXISTS heal_logs (
    id            UUID PRIMARY KEY DEFAULT gen_random_uuid(),
    triggered_by  TEXT NOT NULL CHECK (triggered_by IN ('user', 'scheduled')),
    fixes_applied JSONB NOT NULL DEFAULT '[]',
    fixes_failed  JSONB NOT NULL DEFAULT '[]',
    started_at    TIMESTAMPTZ NOT NULL DEFAULT now(),
    completed_at  TIMESTAMPTZ,
    status        TEXT NOT NULL CHECK (status IN ('success', 'partial', 'failed'))
);

CREATE INDEX IF NOT EXISTS idx_heal_logs_started_at ON heal_logs(started_at DESC);
