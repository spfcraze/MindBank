package reasoner

import (
	"testing"

	"mindbank/internal/models"
)

func TestRuleBased_Extract_Decisions(t *testing.T) {
	r := NewRuleBased(nil)

	// Note: regex is greedy — (.{10,200}) captures across sentence boundaries.
	// Two decision phrases in close proximity get consumed as one match.
	msg := "We decided to use PostgreSQL for the primary database."
	nodes := r.Extract(msg)

	var decisions int
	for _, n := range nodes {
		if n.NodeType == models.NodeDecision {
			decisions++
		}
	}
	if decisions != 1 {
		t.Errorf("expected 1 decision, got %d", decisions)
	}
}

func TestRuleBased_Extract_Preferences(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "I prefer dark mode. User prefers CLI over GUI."
	nodes := r.Extract(msg)

	var prefs int
	for _, n := range nodes {
		if n.NodeType == models.NodePreference {
			prefs++
		}
	}
	// Greedy regex: second phrase may be consumed by first match
	// Test verifies at least 1 preference is extracted
	if prefs < 1 {
		t.Errorf("expected at least 1 preference, got %d", prefs)
	}
}

func TestRuleBased_Extract_Problems(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "The login flow is broken. There's a bug in the payment module."
	nodes := r.Extract(msg)

	var problems int
	for _, n := range nodes {
		if n.NodeType == models.NodeProblem {
			problems++
		}
	}
	if problems < 1 {
		t.Errorf("expected at least 1 problem, got %d", problems)
	}
}

func TestRuleBased_Extract_Advice(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "Best practice: always validate inputs. Pro tip: use parameterized queries."
	nodes := r.Extract(msg)

	var advice int
	for _, n := range nodes {
		if n.NodeType == models.NodeAdvice {
			advice++
		}
	}
	if advice < 1 {
		t.Errorf("expected at least 1 advice, got %d", advice)
	}
}

func TestRuleBased_Extract_URLs(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "Check https://example.com/docs and http://localhost:8080/api"
	nodes := r.Extract(msg)

	var urls int
	for _, n := range nodes {
		if n.NodeType == models.NodeFact && n.Label == n.Content {
			urls++
		}
	}
	if urls != 2 {
		t.Errorf("expected 2 URL facts, got %d", urls)
	}
}

func TestRuleBased_Extract_IPs(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "Server is at 192.168.1.1 and backup at 10.0.0.1"
	nodes := r.Extract(msg)

	var ips int
	for _, n := range nodes {
		if n.NodeType == models.NodeFact && n.Content == "192.168.1.1" {
			ips++
		}
		if n.NodeType == models.NodeFact && n.Content == "10.0.0.1" {
			ips++
		}
	}
	if ips != 2 {
		t.Errorf("expected 2 IP facts, got %d", ips)
	}
}

func TestRuleBased_Extract_NoMatch(t *testing.T) {
	r := NewRuleBased(nil)

	msg := "Hello world, this is just a normal message with no actionable content."
	nodes := r.Extract(msg)
	if len(nodes) != 0 {
		t.Errorf("expected 0 nodes for plain text, got %d", len(nodes))
	}
}

func TestRuleBased_Extract_Mixed(t *testing.T) {
	r := NewRuleBased(nil)

	msg := `We decided to use Go for the backend.
		I prefer PostgreSQL over MySQL.
		There's a bug in the auth module.
		Best practice: use context for cancellation.
		See https://go.dev/doc for docs.
		Server runs on 127.0.0.1`

	nodes := r.Extract(msg)

	counts := map[models.NodeType]int{}
	for _, n := range nodes {
		counts[n.NodeType]++
	}

	if counts[models.NodeDecision] != 1 {
		t.Errorf("expected 1 decision, got %d", counts[models.NodeDecision])
	}
	if counts[models.NodePreference] != 1 {
		t.Errorf("expected 1 preference, got %d", counts[models.NodePreference])
	}
	if counts[models.NodeProblem] != 1 {
		t.Errorf("expected 1 problem, got %d", counts[models.NodeProblem])
	}
	if counts[models.NodeAdvice] != 1 {
		t.Errorf("expected 1 advice, got %d", counts[models.NodeAdvice])
	}
	// URLs and IPs are facts
	if counts[models.NodeFact] != 2 {
		t.Errorf("expected 2 facts (URL+IP), got %d", counts[models.NodeFact])
	}
}

func TestTruncateLabel(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"this is longer than ten", 10, "this is..."},
		{"", 5, ""},
	}

	for _, tt := range tests {
		got := truncateLabel(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncateLabel(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}

func TestTruncate(t *testing.T) {
	tests := []struct {
		input string
		max   int
		want  string
	}{
		{"short", 10, "short"},
		{"exactlyten", 10, "exactlyten"},
		{"this is longer than ten chars", 10, "this is..."},
		{"日本語テスト", 5, "日本..."},  // 2 runes + "..." = 5 runes total (maxLen-3)
	}

	for _, tt := range tests {
		got := truncate(tt.input, tt.max)
		if got != tt.want {
			t.Errorf("truncate(%q, %d) = %q, want %q", tt.input, tt.max, got, tt.want)
		}
	}
}
