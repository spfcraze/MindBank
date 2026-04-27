package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
)

// SettingsRepo handles key-value settings storage.
type SettingsRepo struct {
	pool *pgxpool.Pool
}

// NewSettingsRepo creates a new settings repository.
func NewSettingsRepo(pool *pgxpool.Pool) *SettingsRepo {
	return &SettingsRepo{pool: pool}
}

// Get retrieves a setting value by key.
func (r *SettingsRepo) Get(ctx context.Context, key string) (string, error) {
	var value string
	err := r.pool.QueryRow(ctx, "SELECT value FROM settings WHERE key = $1", key).Scan(&value)
	if err != nil {
		return "", fmt.Errorf("settings get %s: %w", key, err)
	}
	return value, nil
}

// Set updates or inserts a setting value.
func (r *SettingsRepo) Set(ctx context.Context, key, value string) error {
	_, err := r.pool.Exec(ctx, `
		INSERT INTO settings (key, value, updated_at) VALUES ($1, $2, now())
		ON CONFLICT (key) DO UPDATE SET value = EXCLUDED.value, updated_at = now()
	`, key, value)
	if err != nil {
		return fmt.Errorf("settings set %s: %w", key, err)
	}
	return nil
}

// GetBool retrieves a setting as boolean (returns false on error or if not "true").
func (r *SettingsRepo) GetBool(ctx context.Context, key string) bool {
	v, _ := r.Get(ctx, key)
	return v == "true"
}

// GetAll retrieves all settings as a map.
func (r *SettingsRepo) GetAll(ctx context.Context) (map[string]string, error) {
	rows, err := r.pool.Query(ctx, "SELECT key, value FROM settings")
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	result := make(map[string]string)
	for rows.Next() {
		var k, v string
		if err := rows.Scan(&k, &v); err != nil {
			continue
		}
		result[k] = v
	}
	return result, nil
}
