#!/bin/bash
# Run all MindBank migrations in order
# Usage: ./run-migrations.sh [database_url]

set -e

DB_URL="${1:-postgres://mindbank:mindbank_secret@localhost:5434/mindbank?sslmode=disable}"
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

# Run each migration in order
for migration in "$MIGRATIONS_DIR"/*.sql; do
    if [ -f "$migration" ]; then
        filename=$(basename "$migration")
        echo "Running: $filename"
        psql "$DB_URL" -f "$migration" -q
        echo "  ✓ Done"
    fi
done

echo ""
echo "All migrations completed successfully!"
