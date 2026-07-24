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
		confirmationCount  int
		status             string
		wantScoreMin       float64
		wantScoreMax       float64
		wantTrust          string
	}{
		{
			name:               "perfect_observed_supported",
			accessCount:        100,
			edgeCount:          100,
			ageDays:            1,
			importance:         1.0,
			evidenceCount:      10,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			confirmationCount:  3,
			status:             "supported",
			wantScoreMin:       0.85,
			wantScoreMax:       1.0,
			wantTrust:          "high",
		},
		{
			name:               "zero_node_open",
			accessCount:        0,
			edgeCount:          0,
			ageDays:            0,
			importance:         0.0,
			evidenceCount:      0,
			epistemicLabel:     "unknown",
			contradictionCount: 0,
			confirmationCount:  0,
			status:             "open",
			wantScoreMin:       0.15,
			wantScoreMax:       0.15,
			wantTrust:          "low",
		},
		{
			name:               "observed_vs_inferred_open",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			confirmationCount: 0,
			status:             "open",
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "inferred_vs_assumed_open",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "inferred",
			contradictionCount: 0,
			confirmationCount: 0,
			status:             "open",
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "assumed_low_open",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "assumed",
			contradictionCount: 0,
			confirmationCount: 0,
			status:             "open",
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "medium",
		},
		{
			name:               "contradictions_penalty_open",
			accessCount:        50,
			edgeCount:          10,
			ageDays:            100,
			importance:         0.8,
			evidenceCount:      5,
			epistemicLabel:     "observed",
			contradictionCount: 3,
			confirmationCount:  0,
			status:             "open",
			wantScoreMin:       0.0,
			wantScoreMax:       1.0,
			wantTrust:          "high",
		},
		{
			name:               "refuted_zero",
			accessCount:        50,
			edgeCount:          10,
			ageDays:            100,
			importance:         0.8,
			evidenceCount:      5,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			confirmationCount:  0,
			status:             "refuted",
			wantScoreMin:       0.0,
			wantScoreMax:       0.5,
			wantTrust:          "low",
		},
		{
			name:               "blocked_zero",
			accessCount:        50,
			edgeCount:          10,
			ageDays:            100,
			importance:         0.8,
			evidenceCount:      5,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			confirmationCount:  0,
			status:             "blocked",
			wantScoreMin:       0.0,
			wantScoreMax:       0.0,
			wantTrust:          "low",
		},
		{
			name:               "supported_boost",
			accessCount:        10,
			edgeCount:          5,
			ageDays:            30,
			importance:         0.5,
			evidenceCount:      2,
			epistemicLabel:     "observed",
			contradictionCount: 0,
			confirmationCount:  3,
			status:             "supported",
			wantScoreMin:       0.5,
			wantScoreMax:       1.0,
			wantTrust:          "high",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			score, trust := calculateConfidence(tt.accessCount, tt.edgeCount, tt.ageDays, tt.importance, tt.evidenceCount, tt.epistemicLabel, tt.contradictionCount, tt.confirmationCount, tt.status)

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
		observedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "open")
		inferredScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "inferred", 0, 0, "open")
		assumedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "assumed", 0, 0, "open")

		if observedScore <= inferredScore {
			t.Errorf("observed score %v should be > inferred score %v", observedScore, inferredScore)
		}
		if inferredScore <= assumedScore {
			t.Errorf("inferred score %v should be > assumed score %v", inferredScore, assumedScore)
		}
	})

	// Status ordering test: supported > open > inconclusive > refuted > blocked
	t.Run("ordering_status", func(t *testing.T) {
		supportedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "supported")
		openScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "open")
		refutedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "refuted")
		blockedScore, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "blocked")

		if supportedScore <= openScore {
			t.Errorf("supported score %v should be > open score %v", supportedScore, openScore)
		}
		if openScore <= refutedScore {
			t.Errorf("open score %v should be > refuted score %v", openScore, refutedScore)
		}
		if refutedScore <= blockedScore {
			t.Errorf("refuted score %v should be > blocked score %v", refutedScore, blockedScore)
		}
		if blockedScore != 0 {
			t.Errorf("blocked score %v should be exactly 0", blockedScore)
		}
	})

	// Confirmation count boost test
	t.Run("confirmation_boost", func(t *testing.T) {
		zeroConf, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 0, "open")
		threeConf, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 3, "open")
		maxConf, _ := calculateConfidence(10, 5, 30, 0.5, 2, "observed", 0, 10, "open")

		if threeConf <= zeroConf {
			t.Errorf("3-conf score %v should be > 0-conf score %v", threeConf, zeroConf)
		}
		if maxConf < threeConf {
			t.Errorf("max-conf score %v should be >= 3-conf score %v", maxConf, threeConf)
		}
	})

	// Zero node exact score test (open status, no confirmation)
	t.Run("zero_node_exact", func(t *testing.T) {
		score, trust := calculateConfidence(0, 0, 0, 0.0, 0, "unknown", 0, 0, "open")
		if math.Abs(score-0.15) > 1e-9 {
			t.Errorf("zero node exact score = %v, want 0.15", score)
		}
		if trust != "low" {
			t.Errorf("zero node trust = %v, want low", trust)
		}
	})
}
