package integration

import (
	"os"
	"path/filepath"
	"strings"
	"testing"

	"mindbank/internal/hooks"
	"mindbank/internal/models"
)

func TestEventCaptureFlow(t *testing.T) {
	// Setup temp directories
	tmpDir := t.TempDir()
	sessionsDir := filepath.Join(tmpDir, "sessions")
	eventsDir := filepath.Join(sessionsDir, "events")
	os.MkdirAll(eventsDir, 0755)

	// Create event writer (simulates Hermes hook)
	writer := hooks.NewEventWriter(eventsDir)

	// Write a sequence of events
	sessionID := "test-session-1"
	writer.WriteEvent(sessionID, models.EventSessionStart, 0, map[string]any{
		"model": "claude-sonnet",
	})
	writer.WriteEvent(sessionID, models.EventUserPromptSubmit, 1, map[string]any{
		"prompt": "How do I deploy to production?",
	})
	writer.WriteEvent(sessionID, models.EventPreToolUse, 2, map[string]any{
		"tool":    "terminal",
		"command": "git push origin main",
	})
	writer.WriteEvent(sessionID, models.EventPostToolUse, 3, map[string]any{
		"tool":   "terminal",
		"result": "success",
	})
	writer.WriteEvent(sessionID, models.EventStop, 4, map[string]any{
		"duration_seconds": 120,
	})

	// Verify event files exist
	sessionEventDir := filepath.Join(eventsDir, sessionID)
	files, err := os.ReadDir(sessionEventDir)
	if err != nil {
		t.Fatalf("Failed to read event dir: %v", err)
	}

	if len(files) != 5 {
		t.Errorf("Expected 5 event files, got %d", len(files))
	}

	// Verify sequence ordering
	for i, f := range files {
		expectedPrefix := "event_"
		if !strings.HasPrefix(f.Name(), expectedPrefix) {
			t.Errorf("File %d: expected prefix %s, got %s", i, expectedPrefix, f.Name())
		}
	}
}

func TestEventWriterInvalidType(t *testing.T) {
	tmpDir := t.TempDir()
	writer := hooks.NewEventWriter(tmpDir)

	// Should still write even with invalid type (validation happens on read)
	err := writer.WriteEvent("sess-1", "invalid_type", 0, map[string]any{})
	if err != nil {
		t.Fatalf("WriteEvent should not fail for invalid type: %v", err)
	}

	path := filepath.Join(tmpDir, "sess-1", "event_0000_invalid_type.json")
	if _, err := os.Stat(path); os.IsNotExist(err) {
		t.Errorf("Event file should be created even for invalid type")
	}
}
