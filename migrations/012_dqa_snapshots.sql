-- 012: DQA Snapshots (graph health trend tracking)
CREATE TABLE IF NOT EXISTS dqa_snapshots (
    id           BIGINT GENERATED ALWAYS AS IDENTITY PRIMARY KEY,
    score        INT NOT NULL CHECK (score >= 0 AND score <= 100),
    total_nodes  INT NOT NULL DEFAULT 0,
    issues_count INT NOT NULL DEFAULT 0,
    created_at   TIMESTAMPTZ NOT NULL DEFAULT now()
);

CREATE INDEX IF NOT EXISTS idx_dqa_snapshots_created ON dqa_snapshots(created_at DESC);
