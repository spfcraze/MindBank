package capture

import (
	"testing"
)

func TestParseSession(t *testing.T) {
	// Create a test session file
	testContent := `# Test Session

**User**
Hello, this is a test message.

**Assistant**
This is a response.

**User**
Another message.
`

	// Write test file
	tmpFile, err := os.CreateTemp("", "session-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	if _, err := tmpFile.WriteString(testContent); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Parse
	svc := &Service{}
	session, err := svc.parseSession(tmpFile.Name())
	if err != nil {
		t.Fatalf("parseSession failed: %v", err)
	}

	if session.Title != "Test Session" {
		t.Errorf("expected title 'Test Session', got '%s'", session.Title)
	}

	if len(session.Messages) != 3 {
		t.Errorf("expected 3 messages, got %d", len(session.Messages))
	}

	if session.Messages[0].Role != "user" {
		t.Errorf("expected first message role 'user', got '%s'", session.Messages[0].Role)
	}

	if session.Messages[1].Role != "assistant" {
		t.Errorf("expected second message role 'assistant', got '%s'", session.Messages[1].Role)
	}
}

func TestFileHash(t *testing.T) {
	// Create test file
	tmpFile, err := os.CreateTemp("", "test-*.md")
	if err != nil {
		t.Fatal(err)
	}
	defer os.Remove(tmpFile.Name())

	content := "test content for hashing"
	if _, err := tmpFile.WriteString(content); err != nil {
		t.Fatal(err)
	}
	tmpFile.Close()

	// Compute hash
	svc := &Service{}
	hash1, err := svc.fileHash(tmpFile.Name())
	if err != nil {
		t.Fatalf("fileHash failed: %v", err)
	}

	if hash1 == "" {
		t.Error("expected non-empty hash")
	}

	// Same file should produce same hash
	hash2, err := svc.fileHash(tmpFile.Name())
	if err != nil {
		t.Fatalf("fileHash failed: %v", err)
	}

	if hash1 != hash2 {
		t.Error("same file should produce same hash")
	}
}
