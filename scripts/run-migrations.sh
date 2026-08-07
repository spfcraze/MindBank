#!/bin/bash
# Run all MindBank migrations in order
# Usage: ./run-migrations.sh [database_url]
#
# Idempotent: uses the same _migrations tracking table as the Go migrator
# (internal/db/migrate.go), so already-applied migrations are skipped and the
# two paths cannot diverge. Previously this script re-executed every file
# with `set -e`, so a second run aborted on the first non-IF-NOT-EXISTS DDL.

set -e

DB_URL="${1:-postgres://mindbank:mindbank@localhost:5436/mindbank?sslmode=disable}"
MIGRATIONS_DIR="$(dirname "$0")/../internal/db/migrations"

echo "Running MindBank migrations..."
echo "Database: $DB_URL"
echo "Migrations: $MIGRATIONS_DIR"
echo ""

# Check if psql is available
if ! command -v psql &> /dev/null; then
    echo "ERROR: psql not found. Install PostgreSQL client."
    exit 1
fi

# Ensure the tracking table exists (same schema the Go migrator uses)
psql "$DB_URL" -q -c "CREATE TABLE IF NOT EXISTS _migrations (
    name       TEXT PRIMARY KEY,
    applied_at TIMESTAMPTZ NOT NULL DEFAULT now()
)"

# Run each unapplied migration in order, recording it in the same transaction
for migration in "$MIGRATIONS_DIR"/*.sql; do
    if [ -f "$migration" ]; then
        filename=$(basename "$migration")
        applied=$(psql "$DB_URL" -tA -c "SELECT EXISTS(SELECT 1 FROM _migrations WHERE name = '$filename')")
        if [ "$applied" = "t" ]; then
            echo "Skipping (already applied): $filename"
            continue
        fi
        echo "Running: $filename"
        psql "$DB_URL" -q -v ON_ERROR_STOP=1 <<EOF
BEGIN;
\i $migration
INSERT INTO _migrations (name) VALUES ('$filename');
COMMIT;
EOF
        echo "  ✓ Done"
    fi
done

echo ""
echo "All migrations completed successfully!"
