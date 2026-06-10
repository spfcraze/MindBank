package models

import (
	"encoding/json"
	"testing"
)

func TestNode_EpistemicLabel(t *testing.T) {
	n := Node{ID: "n1", EpistemicLabel: "hypothesis"}

	// Verify struct field
	if n.EpistemicLabel != "hypothesis" {
		t.Fatalf("expected EpistemicLabel 'hypothesis', got %q", n.EpistemicLabel)
	}

	// Verify JSON round-trip
	b, err := json.Marshal(n)
	if err != nil {
		t.Fatalf("marshal failed: %v", err)
	}

	var got Node
	if err := json.Unmarshal(b, &got); err != nil {
		t.Fatalf("unmarshal failed: %v", err)
	}
	if got.EpistemicLabel != "hypothesis" {
		t.Fatalf("expected JSON round-trip EpistemicLabel 'hypothesis', got %q", got.EpistemicLabel)
	}

	// Verify omitempty: zero value should omit from JSON
	n2 := Node{ID: "n2"}
	b2, err := json.Marshal(n2)
	if err != nil {
		t.Fatalf("marshal of empty label failed: %v", err)
	}
	var raw map[string]interface{}
	if err := json.Unmarshal(b2, &raw); err != nil {
		t.Fatalf("unmarshal to raw failed: %v", err)
	}
	if _, ok := raw["epistemic_label"]; ok {
		t.Fatalf("expected epistemic_label to be omitted when empty, but found in JSON: %s", b2)
	}
}
