-- 071: Ensure enum values the code relies on exist.
-- During migration renumbering, the ALTER TYPE statements for 'session'
-- (node_type) and the epistemic/temporal edge types were lost from the
-- migration set. Live databases kept them from older migrations; fresh
-- installs lacked them, so search/jspace queries and edge creation failed.
-- ADD VALUE IF NOT EXISTS is a safe no-op where they already exist.
DO $$ BEGIN
    ALTER TYPE node_type ADD VALUE IF NOT EXISTS 'session';
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;

DO $$ BEGIN
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'tested_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'invalidated_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'derived_from';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'assumed';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'superseded_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'refined_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'merged_into';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'created_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'reviewed_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'executed_by';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'failed_due_to';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'incompatible_with';
    ALTER TYPE edge_type ADD VALUE IF NOT EXISTS 'precondition_for';
EXCEPTION WHEN duplicate_object THEN NULL;
END $$;
