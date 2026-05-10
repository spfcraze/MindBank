package autocapture

import (
	"testing"
)

func TestParseEvent(t *testing.T) {
	eventData := []byte(`{
		"session_id": "test-sess-1",
		"event_type": "user_prompt_submit",
		"sequence": 2,
		"timestamp": "2026-05-08T18:00:00Z",
		"payload": {"prompt": "how do I deploy?"}
	}`)

	w := &Watcher{}
	event, err := w.parseEvent(eventData)
	if err != nil {
		t.Fatalf("parseEvent failed: %v", err)
	}

	if event.SessionID != "test-sess-1" {
		t.Errorf("SessionID = %s, want test-sess-1", event.SessionID)
	}
	if event.EventType != "user_prompt_submit" {
		t.Errorf("EventType = %s, want user_prompt_submit", event.EventType)
	}
	if event.Sequence != 2 {
		t.Errorf("Sequence = %d, want 2", event.Sequence)
	}
}

func TestParseEventInvalidType(t *testing.T) {
	eventData := []byte(`{
		"session_id": "test-sess-1",
		"event_type": "invalid_type",
		"sequence": 0,
		"timestamp": "2026-05-08T18:00:00Z",
		"payload": {}
	}`)

	w := &Watcher{}
	event, err := w.parseEvent(eventData)
	if err != nil {
		t.Fatalf("parseEvent failed: %v", err)
	}

	// parseEvent itself doesn't validate — validation happens in ProcessEventFile
	if event.EventType != "invalid_type" {
		t.Errorf("EventType = %s, want invalid_type", event.EventType)
	}
}

func TestParseEventMissingTimestamp(t *testing.T) {
	eventData := []byte(`{
		"session_id": "test-sess-2",
		"event_type": "session_start",
		"sequence": 0,
		"payload": {"model": "claude"}
	}`)

	w := &Watcher{}
	event, err := w.parseEvent(eventData)
	if err != nil {
		t.Fatalf("parseEvent failed: %v", err)
	}

	if event.Timestamp.IsZero() {
		t.Error("Expected non-zero timestamp fallback")
	}
}
