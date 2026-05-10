package repository

import (
	"context"
	"testing"

	"github.com/jackc/pgx/v5/pgxpool"
	"mindbank/internal/models"
)

func setupTestProfileRepo(t *testing.T) *ProfileRepo {
	dsn := "postgres://mindbank:${MB_POSTGRES_PASSWORD:-mindbank_test}@localhost:5434/mindbank?sslmode=disable"
	pool, err := pgxpool.New(context.Background(), dsn)
	if err != nil {
		t.Skipf("DB not available: %v", err)
	}
	return &ProfileRepo{Pool: pool}
}

func TestProfileRepo_Create(t *testing.T) {
	r := setupTestProfileRepo(t)
	ctx := context.Background()

	req := models.ProfileCreate{
		Category: models.ProfileFact,
		Fact:     "User prefers dark mode interfaces",
		Confidence: 0.9,
	}

	p, err := r.Create(ctx, req)
	if err != nil {
		t.Fatalf("Create: %v", err)
	}
	if p.ID == "" {
		t.Fatal("expected ID")
	}
	if p.Fact != req.Fact {
		t.Errorf("fact = %q, want %q", p.Fact, req.Fact)
	}
	if p.Confidence != 0.9 {
		t.Errorf("confidence = %v, want 0.9", p.Confidence)
	}
	if p.ValidTo != nil {
		t.Error("new profile should not be superseded")
	}
}

func TestProfileRepo_ListByCategory(t *testing.T) {
	r := setupTestProfileRepo(t)
	ctx := context.Background()

	// Create test profiles
	_, _ = r.Create(ctx, models.ProfileCreate{Category: models.ProfilePreference, Fact: "pref1", Confidence: 0.8})
	_, _ = r.Create(ctx, models.ProfileCreate{Category: models.ProfilePreference, Fact: "pref2", Confidence: 0.6})
	_, _ = r.Create(ctx, models.ProfileCreate{Category: models.ProfileFact, Fact: "fact1", Confidence: 0.9})

	prefs, err := r.ListByCategory(ctx, models.ProfilePreference)
	if err != nil {
		t.Fatalf("ListByCategory: %v", err)
	}
	if len(prefs) < 2 {
		t.Errorf("got %d preferences, want >= 2", len(prefs))
	}
	// Should be sorted by confidence DESC
	if len(prefs) >= 2 && prefs[0].Confidence < prefs[1].Confidence {
		t.Error("expected sorted by confidence DESC")
	}
}

func TestProfileRepo_Delete(t *testing.T) {
	r := setupTestProfileRepo(t)
	ctx := context.Background()

	p, _ := r.Create(ctx, models.ProfileCreate{Category: models.ProfileGoal, Fact: "goal1"})

	err := r.Delete(ctx, p.ID)
	if err != nil {
		t.Fatalf("Delete: %v", err)
	}

	// After delete, should not appear in ListAll
	all, _ := r.ListAll(ctx)
	for _, prof := range all {
		if prof.ID == p.ID {
			t.Fatal("deleted profile still in list")
		}
	}
}

func TestProfileRepo_GetContextForQuery(t *testing.T) {
	r := setupTestProfileRepo(t)
	ctx := context.Background()

	_, _ = r.Create(ctx, models.ProfileCreate{Category: models.ProfileFact, Fact: "Uses Go for backend"})
	_, _ = r.Create(ctx, models.ProfileCreate{Category: models.ProfilePreference, Fact: "Likes minimal UIs"})

	result, err := r.GetContextForQuery(ctx, "tech stack")
	if err != nil {
		t.Fatalf("GetContextForQuery: %v", err)
	}
	if result == "" {
		t.Fatal("expected context string")
	}
	if !contains(result, "Uses Go for backend") {
		t.Error("expected fact in context")
	}
}

func contains(s, substr string) bool {
	return len(s) >= len(substr) && (s == substr || len(s) > 0 && containsHelper(s, substr))
}

func containsHelper(s, substr string) bool {
	for i := 0; i <= len(s)-len(substr); i++ {
		if s[i:i+len(substr)] == substr {
			return true
		}
	}
	return false
}
