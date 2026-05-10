package models

import "testing"

func TestEventTypeConstants(t *testing.T) {
	if EventSessionStart != "session_start" {
		t.Errorf("EventSessionStart = %s, want session_start", EventSessionStart)
	}
	if EventUserPromptSubmit != "user_prompt_submit" {
		t.Errorf("EventUserPromptSubmit = %s, want user_prompt_submit", EventUserPromptSubmit)
	}
	if EventPreToolUse != "pre_tool_use" {
		t.Errorf("EventPreToolUse = %s, want pre_tool_use", EventPreToolUse)
	}
	if EventPostToolUse != "post_tool_use" {
		t.Errorf("EventPostToolUse = %s, want post_tool_use", EventPostToolUse)
	}
	if EventStop != "stop" {
		t.Errorf("EventStop = %s, want stop", EventStop)
	}
}

func TestEventNodeValidation(t *testing.T) {
	e := EventNode{
		SessionID: "sess-123",
		EventType: EventUserPromptSubmit,
		Sequence:  1,
		Payload:   []byte(`{"prompt":"hello"}`),
	}
	if e.SessionID == "" {
		t.Error("SessionID required")
	}
	if e.EventType == "" {
		t.Error("EventType required")
	}
}

func TestValidEventTypes(t *testing.T) {
	types := ValidEventTypes()
	if len(types) != 5 {
		t.Errorf("ValidEventTypes() returned %d types, want 5", len(types))
	}
	expected := map[string]bool{
		"session_start":     true,
		"user_prompt_submit": true,
		"pre_tool_use":       true,
		"post_tool_use":      true,
		"stop":               true,
	}
	for _, et := range types {
		if !expected[et] {
			t.Errorf("Unexpected event type: %s", et)
		}
	}
}
