package handler

import (
	"math"
	"testing"
)

func TestCalculateConfidence(t *testing.T) {
	tests := []struct {
		name               string
		accessCount        int
		edgeCount          int
		ageDays            int
		importance         float64
		evidenceCount      int
		epistemicLabel     string
		contradictionCount int
		wantScoreMin       float64
		wantScoreMax       float64
		wantTrust          string
	}{
		{
			name:               "perfect_observed",
			accessCount:        100,
			edgeCount:          100,
			ageDays:            1,
			importance:         1.0,
			evidenceCount:      10,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			wantScoreMin:       0.85,
			wantScoreMax:       1.0,
			wantTrust:          "high",
		},
		{
			name:               "zero_node",
			accessCount:        0,
			edgeCount:          0,
			ageDays:            0,
			importance:         0.0,
			evidenceCount:      0,
			epistemicLabel:     "unknown",
			contradictionCount: 0,
			wantScoreMin:       0.15,
			wantScoreMax:       0.15,
			wantTrust:          "low",
		},
		{
			name:               "observed_vs_inferred",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "inferred_vs_assumed",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "inferred",
			contradictionCount: 0,
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "assumed_low",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "assumed",
			contradictionCount: 0,
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "contradictions_penalty",
			accessCount:        50,
			edgeCount:          10,
			ageDays:            100,
			importance:         0.8,
			evidenceCount:      5,
			epistemicLabel:     "observed",
			contradictionCount: 3,
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, trust := calculateConfidence(tt.accessCount, tt.edgeCount, tt.ageDays, tt.importance, tt.evidenceCount, tt.epistemicLabel, tt.contradictionCount)

			if score < tt.wantScoreMin || score > tt.wantScoreMax {
				t.Errorf("calculateConfidence() score = %v, want between %v and %v", score, tt.wantScoreMin, tt.wantScoreMax)
			}
			if trust != tt.wantTrust {
				t.Errorf("calculateConfidence() trust = %v, want %v", trust, tt.wantTrust)
			}
		})
	}

	// Specific ordering test: observed > inferred > assumed with same topology
	t.Run("ordering_observed_gt_inferred_gt_assumed", func(t *testing.T) {
		observedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0)
		inferredScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "inferred", 0)
		assumedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "assumed", 0)

		if observedScore <= inferredScore {
			t.Errorf("observed score %v should be > inferred score %v", observedScore, inferredScore)
		}
		if inferredScore <= assumedScore {
			t.Errorf("inferred score %v should be > assumed score %v", inferredScore, assumedScore)
		}
	})

	// Zero node exact score test
	t.Run("zero_node_exact", func(t *testing.T) {
		score, trust := calculateConfidence(0, 0, 0, 0.0, 0, "unknown", 0)
		if math.Abs(score-0.15) > 1e-9 {
			t.Errorf("zero node exact score = %v, want 0.15", score)
		}
		if trust != "low" {
			t.Errorf("zero node trust = %v, want low", trust)
		}
	})
}
