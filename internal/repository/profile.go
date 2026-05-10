package repository

import (
	"context"
	"fmt"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/models"
)

// ProfileRepo handles profile CRUD.
type ProfileRepo struct {
	Pool *pgxpool.Pool
}

// Create inserts a new profile.
func (r *ProfileRepo) Create(ctx context.Context, req models.ProfileCreate) (*models.Profile, error) {
	meta := req.Metadata
	if meta == nil {
		meta = []byte("{}")
	}
	conf := float32(0.5)
	if req.Confidence != 0 {
		conf = req.Confidence
	}

	p := &models.Profile{}
	err := r.Pool.QueryRow(ctx, `
		INSERT INTO profiles (category, fact, confidence, source_node_id, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, category, fact, confidence, source_node_id, valid_from, valid_to,
		          metadata, created_at, updated_at
	`, req.Category, req.Fact, conf, req.SourceNodeID, meta,
	).Scan(&p.ID, &p.Category, &p.Fact, &p.Confidence, &p.SourceNodeID,
		&p.ValidFrom, &p.ValidTo, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert profile: %w", err)
	}
	return p, nil
}

// Get fetches a profile by ID.
func (r *ProfileRepo) Get(ctx context.Context, id string) (*models.Profile, error) {
	p := &models.Profile{}
	err := r.Pool.QueryRow(ctx, `
		SELECT id, category, fact, confidence, source_node_id, valid_from, valid_to,
		       metadata, created_at, updated_at
		FROM profiles WHERE id = $1
	`, id).Scan(&p.ID, &p.Category, &p.Fact, &p.Confidence, &p.SourceNodeID,
		&p.ValidFrom, &p.ValidTo, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get profile: %w", err)
	}
	return p, nil
}

// ListByCategory returns current profiles for a category.
func (r *ProfileRepo) ListByCategory(ctx context.Context, category models.ProfileCategory) ([]models.Profile, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT id, category, fact, confidence, source_node_id, valid_from, valid_to,
		       metadata, created_at, updated_at
		FROM profiles
		WHERE category = $1 AND valid_to IS NULL
		ORDER BY confidence DESC, created_at DESC
	`, category)
	if err != nil {
		return nil, fmt.Errorf("list profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		err := rows.Scan(&p.ID, &p.Category, &p.Fact, &p.Confidence, &p.SourceNodeID,
			&p.ValidFrom, &p.ValidTo, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// ListAll returns all current profiles.
func (r *ProfileRepo) ListAll(ctx context.Context) ([]models.Profile, error) {
	rows, err := r.Pool.Query(ctx, `
		SELECT id, category, fact, confidence, source_node_id, valid_from, valid_to,
		       metadata, created_at, updated_at
		FROM profiles
		WHERE valid_to IS NULL
		ORDER BY confidence DESC, created_at DESC
	`)
	if err != nil {
		return nil, fmt.Errorf("list all profiles: %w", err)
	}
	defer rows.Close()

	var profiles []models.Profile
	for rows.Next() {
		var p models.Profile
		err := rows.Scan(&p.ID, &p.Category, &p.Fact, &p.Confidence, &p.SourceNodeID,
			&p.ValidFrom, &p.ValidTo, &p.Metadata, &p.CreatedAt, &p.UpdatedAt)
		if err != nil {
			continue
		}
		profiles = append(profiles, p)
	}
	return profiles, nil
}

// Update modifies a profile's fact/confidence.
func (r *ProfileRepo) Update(ctx context.Context, id string, req models.ProfileUpdate) (*models.Profile, error) {
	tx, err := r.Pool.Begin(ctx)
	if err != nil {
		return nil, fmt.Errorf("begin tx: %w", err)
	}
	defer tx.Rollback(ctx)

	// Get existing
	existing := &models.Profile{}
	err = tx.QueryRow(ctx, `
		SELECT id, category, fact, confidence, source_node_id, valid_from, valid_to,
		       metadata, created_at, updated_at
		FROM profiles WHERE id = $1
	`, id).Scan(&existing.ID, &existing.Category, &existing.Fact, &existing.Confidence,
		&existing.SourceNodeID, &existing.ValidFrom, &existing.ValidTo,
		&existing.Metadata, &existing.CreatedAt, &existing.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("get existing: %w", err)
	}

	// Mark old version as superseded
	_, err = tx.Exec(ctx, `UPDATE profiles SET valid_to = now() WHERE id = $1`, id)
	if err != nil {
		return nil, fmt.Errorf("supersede: %w", err)
	}

	// Create new version
	fact := existing.Fact
	if req.Fact != nil {
		fact = *req.Fact
	}
	conf := existing.Confidence
	if req.Confidence != nil {
		conf = *req.Confidence
	}
	meta := existing.Metadata
	if req.Metadata != nil {
		meta = req.Metadata
	}

	newProfile := &models.Profile{}
	err = tx.QueryRow(ctx, `
		INSERT INTO profiles (category, fact, confidence, source_node_id, metadata)
		VALUES ($1, $2, $3, $4, $5)
		RETURNING id, category, fact, confidence, source_node_id, valid_from, valid_to,
		          metadata, created_at, updated_at
	`, existing.Category, fact, conf, existing.SourceNodeID, meta,
	).Scan(&newProfile.ID, &newProfile.Category, &newProfile.Fact, &newProfile.Confidence,
		&newProfile.SourceNodeID, &newProfile.ValidFrom, &newProfile.ValidTo,
		&newProfile.Metadata, &newProfile.CreatedAt, &newProfile.UpdatedAt)
	if err != nil {
		return nil, fmt.Errorf("insert new: %w", err)
	}

	if err := tx.Commit(ctx); err != nil {
		return nil, fmt.Errorf("commit: %w", err)
	}
	return newProfile, nil
}

// Delete marks a profile as superseded (soft delete).
func (r *ProfileRepo) Delete(ctx context.Context, id string) error {
	_, err := r.Pool.Exec(ctx, `UPDATE profiles SET valid_to = now() WHERE id = $1`, id)
	if err != nil {
		return fmt.Errorf("delete profile: %w", err)
	}
	return nil
}

// GetContextForQuery returns profile facts as context string for search augmentation.
func (r *ProfileRepo) GetContextForQuery(ctx context.Context, query string) (string, error) {
	profiles, err := r.ListAll(ctx)
	if err != nil {
		return "", err
	}
	if len(profiles) == 0 {
		return "", nil
	}

	var result string
	for _, p := range profiles {
		result += fmt.Sprintf("[%s] %s\n", p.Category, p.Fact)
	}
	return result, nil
}
