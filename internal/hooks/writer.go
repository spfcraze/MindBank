package hooks

import (
	"encoding/json"
	"fmt"
	"os"
	"path/filepath"
	"time"
)

// EventWriter writes lifecycle events to the events directory.
type EventWriter struct {
	eventsDir string
}

// NewEventWriter creates a writer that outputs to the given directory.
func NewEventWriter(eventsDir string) *EventWriter {
	return &EventWriter{eventsDir: eventsDir}
}

// WriteEvent writes a single lifecycle event as JSON.
func (w *EventWriter) WriteEvent(sessionID, eventType string, sequence int, payload map[string]any) error {
	// Create session subdirectory
	sessionDir := filepath.Join(w.eventsDir, sessionID)
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		return fmt.Errorf("create session dir: %w", err)
	}

	event := map[string]any{
		"session_id": sessionID,
		"event_type": eventType,
		"sequence":   sequence,
		"timestamp":  time.Now().UTC().Format(time.RFC3339Nano),
		"payload":    payload,
	}

	data, err := json.MarshalIndent(event, "", "  ")
	if err != nil {
		return fmt.Errorf("marshal event: %w", err)
	}

	filename := fmt.Sprintf("event_%04d_%s.json", sequence, eventType)
	path := filepath.Join(sessionDir, filename)

	if err := os.WriteFile(path, data, 0644); err != nil {
		return fmt.Errorf("write event file: %w", err)
	}

	return nil
}
