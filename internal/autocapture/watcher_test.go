package autocapture

import (
	"strings"
	"testing"
)

func TestDeriveLabelFromContent(t *testing.T) {
	// Simulate session JSON with no title field
	// Use timestamp filename with dashes (YYYY-MM-DD format) to trigger content extraction
	data := []byte(`{"messages":[
        {"role":"user","content":"How do I deploy the API to production with Docker?"},
        {"role":"assistant","content":"You can use Docker Compose with the production profile..."},
        {"role":"user","content":"What about environment variables?"}
    ]}`)

	w := &Watcher{}
	label := w.deriveLabel("session_2026-04-08_192513.json", data)

	// Should NOT be the timestamp filename
	if strings.HasPrefix(label, "session_2026") {
		t.Errorf("Expected generated title, got filename: %s", label)
	}

	// Should contain topic-related words
	lower := strings.ToLower(label)
	if !strings.Contains(lower, "deploy") && !strings.Contains(lower, "docker") && !strings.Contains(lower, "api") {
		t.Errorf("Title should reflect content topic, got: %s", label)
	}

	// Should be reasonable length
	if len(label) < 5 || len(label) > 80 {
		t.Errorf("Title length unreasonable: %d chars", len(label))
	}
}

func TestDeriveLabelWithExistingTitle(t *testing.T) {
	// Session that already has a title
	data := []byte(`{"title":"Docker Deployment Guide","messages":[]}`)

	w := &Watcher{}
	label := w.deriveLabel("session_2026-04-08_192513.json", data)

	if label != "Docker Deployment Guide" {
		t.Errorf("Expected existing title, got: %s", label)
	}
}

func TestDeriveLabelShortContent(t *testing.T) {
	// Very short session — should fallback to filename
	data := []byte(`{"messages":[{"role":"user","content":"Hi"}]}`)

	w := &Watcher{}
	label := w.deriveLabel("session_2026-04-08_192513.json", data)

	// Should fallback to something reasonable
	if label == "" {
		t.Error("Expected non-empty label for short content")
	}
}

func TestExtractTopic(t *testing.T) {
	data := []byte(`{"messages":[
        {"role":"user","content":"How do I fix the postgres connection timeout?"},
        {"role":"assistant","content":"You need to increase the connection pool size..."}
    ]}`)

	w := &Watcher{}
	topic := w.extractTopic(data)

	if topic != "database" && topic != "bugfix" {
		t.Errorf("Expected database or bugfix topic, got: %s", topic)
	}
}

func TestQualityGates(t *testing.T) {
	w := &Watcher{}

	// Test low quality labels
	if !w.isLowQualityLabel("Sim: test session") {
		t.Error("expected Sim: prefix to be low quality")
	}
	if !w.isLowQualityLabel("worker session") {
		t.Error("expected worker to be low quality")
	}
	if !w.isLowQualityLabel("Test: deployment") {
		t.Error("expected Test: prefix to be low quality")
	}
	if w.isLowQualityLabel("Deploy Kubernetes to production") {
		t.Error("expected meaningful label to pass")
	}

	// Test meaningful content
	emptyData := []byte(`{"messages":[{"role":"user","content":"hi"}]}`)
	if w.hasMeaningfulContent(emptyData) {
		t.Error("expected empty content to be rejected")
	}

	goodData := []byte(`{"messages":[{"role":"user","content":"This is a meaningful request about deploying Kubernetes clusters to production with proper monitoring"}]}`)
	if !w.hasMeaningfulContent(goodData) {
		t.Error("expected meaningful content to pass")
	}
}
