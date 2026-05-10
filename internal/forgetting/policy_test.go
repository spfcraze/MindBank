package forgetting

import (
	"testing"
	"time"

	"mindbank/internal/models"
)

func TestGetDefaultTTL(t *testing.T) {
	tests := []struct {
		nodeType models.NodeType
		want     time.Duration
	}{
		{models.NodeSession, 30 * 24 * time.Hour},
		{models.NodeFact, 90 * 24 * time.Hour},
		{models.NodePreference, 365 * 24 * time.Hour},
		{models.NodeDecision, 180 * 24 * time.Hour},
		{models.NodeProblem, 60 * 24 * time.Hour},
		{models.NodeAdvice, 90 * 24 * time.Hour},
		{models.NodeConcept, 120 * 24 * time.Hour},
		{models.NodeQuestion, 30 * 24 * time.Hour},
		{models.NodeTopic, 60 * 24 * time.Hour},
		{models.NodePerson, 0},
		{models.NodeProject, 0},
		{"unknown", 90 * 24 * time.Hour}, // fallback
	}

	for _, tt := range tests {
		got := GetDefaultTTL(tt.nodeType)
		if got != tt.want {
			t.Errorf("GetDefaultTTL(%q) = %v, want %v", tt.nodeType, got, tt.want)
		}
	}
}

func TestGetTTL_Pinned(t *testing.T) {
	meta := []byte(`{"important":true}`)
	n := &models.Node{
		NodeType: models.NodeSession,
		Metadata: meta,
	}

	ttl := GetTTL(n)
	if ttl != 0 {
		t.Errorf("pinned node TTL = %v, want 0 (never expires)", ttl)
	}
}

func TestGetTTL_Unpinned(t *testing.T) {
	meta := []byte(`{"important":false}`)
	n := &models.Node{
		NodeType: models.NodeFact,
		Metadata: meta,
	}

	ttl := GetTTL(n)
	if ttl != 90*24*time.Hour {
		t.Errorf("unpinned fact TTL = %v, want %v", ttl, 90*24*time.Hour)
	}
}

func TestCalculateExpiry_NeverExpires(t *testing.T) {
	n := &models.Node{
		NodeType: models.NodePerson,
	}

	expiry := CalculateExpiry(n)
	if expiry != nil {
		t.Errorf("never-expire node got expiry = %v, want nil", expiry)
	}
}

func TestCalculateExpiry_HasExpiry(t *testing.T) {
	n := &models.Node{
		NodeType: models.NodeFact,
	}

	before := time.Now()
	expiry := CalculateExpiry(n)
	after := time.Now().Add(91 * 24 * time.Hour)

	if expiry == nil {
		t.Fatal("expected expiry for fact node")
	}
	if expiry.Before(before) || expiry.After(after) {
		t.Errorf("expiry = %v, want between %v and %v", *expiry, before, after)
	}
}
