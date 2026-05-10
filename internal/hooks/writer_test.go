package hooks

import (
	"encoding/json"
	"os"
	"path/filepath"
	"strings"
	"testing"
)

func TestWriteEvent(t *testing.T) {
	tmpDir := t.TempDir()
	writer := NewEventWriter(tmpDir)

	err := writer.WriteEvent("sess-123", "user_prompt_submit", 1, map[string]any{
		"prompt": "hello world",
	})
	if err != nil {
		t.Fatalf("WriteEvent failed: %v", err)
	}

	// Check file was created
	eventFile := filepath.Join(tmpDir, "sess-123", "event_0001_user_prompt_submit.json")
	if _, err := os.Stat(eventFile); os.IsNotExist(err) {
		t.Errorf("Event file not created: %s", eventFile)
	}
}

func TestEventFileFormat(t *testing.T) {
	tmpDir := t.TempDir()
	writer := NewEventWriter(tmpDir)

	writer.WriteEvent("sess-abc", "session_start", 0, map[string]any{
		"model": "claude-sonnet",
	})

	eventFile := filepath.Join(tmpDir, "sess-abc", "event_0000_session_start.json")
	data, _ := os.ReadFile(eventFile)

	// Must be valid JSON
	var event map[string]any
	if err := json.Unmarshal(data, &event); err != nil {
		t.Errorf("Event file is not valid JSON: %v", err)
	}

	if event["session_id"] != "sess-abc" {
		t.Errorf("session_id = %v, want sess-abc", event["session_id"])
	}
	if event["event_type"] != "session_start" {
		t.Errorf("event_type = %v, want session_start", event["event_type"])
	}
	if seq, ok := event["sequence"].(float64); !ok || seq != 0 {
		t.Errorf("sequence = %v, want 0", event["sequence"])
	}
}

func TestWriteEventSequenceOrdering(t *testing.T) {
	tmpDir := t.TempDir()
	writer := NewEventWriter(tmpDir)

	// Write events out of order
	writer.WriteEvent("sess-order", "stop", 2, map[string]any{"done": true})
	writer.WriteEvent("sess-order", "session_start", 0, map[string]any{"start": true})
	writer.WriteEvent("sess-order", "user_prompt_submit", 1, map[string]any{"prompt": "hi"})

	// Verify all files exist
	expectedFiles := []string{
		"event_0002_stop.json",
		"event_0000_session_start.json",
		"event_0001_user_prompt_submit.json",
	}

	sessionDir := filepath.Join(tmpDir, "sess-order")
	for _, fname := range expectedFiles {
		path := filepath.Join(sessionDir, fname)
		if _, err := os.Stat(path); os.IsNotExist(err) {
			t.Errorf("Missing event file: %s", fname)
		}
	}

	// Verify lexical sort matches sequence order
	entries, _ := os.ReadDir(sessionDir)
	var names []string
	for _, e := range entries {
		if !e.IsDir() && strings.HasSuffix(e.Name(), ".json") {
			names = append(names, e.Name())
		}
	}

	// Should be sorted: 0000, 0001, 0002
	if len(names) >= 3 {
		if !strings.HasPrefix(names[0], "event_0000") {
			t.Errorf("First file should be sequence 0, got: %s", names[0])
		}
	}
}
