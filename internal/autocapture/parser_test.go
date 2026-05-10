package autocapture

import (
	"testing"
)

func TestDeriveNamespaceFromPath(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		{"/home/rat/mindbank", "mindbank"},
		{"/home/rat/mindbank/", "mindbank"},
		{"/home/rat/projects/klixsor", "klixsor"},
		{"/home/rat", "rat"},
		{"", "global"},
		{"/", "global"},
		{"  /home/rat/mindbank  ", "mindbank"}, // whitespace trimming
		{"/home/rat/mindbank//", "mindbank"},   // multiple trailing slashes
		{"/a", "a"},
		{"/a/", "a"},
		{"a", "a"},
		{"a/", "a"},
		{".", "global"},
		{"..", ".."},
		{"/home/rat/.hidden", ".hidden"},
		{"   ", "global"}, // whitespace only
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := DeriveNamespaceFromPath(tt.path)
			if got != tt.expected {
				t.Errorf("DeriveNamespaceFromPath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestParseSessionForNamespace(t *testing.T) {
	t.Run("working_directory field", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/mindbank","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("cwd field", func(t *testing.T) {
		sessionJSON := `{"cwd":"/home/rat/projects/klixsor","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "klixsor" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "klixsor")
		}
	})

	t.Run("working_directory takes precedence over cwd", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/mindbank","cwd":"/home/rat/projects/other","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("empty fields fallback to global", func(t *testing.T) {
		sessionJSON := `{"messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("empty working_directory with cwd present", func(t *testing.T) {
		sessionJSON := `{"working_directory":"","cwd":"/home/rat/mindbank","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("root path fallback to global", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("trailing slash path", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/mindbank/","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("invalid json returns error", func(t *testing.T) {
		sessionJSON := `{"working_directory":}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err == nil {
			t.Fatal("expected error for invalid JSON, got nil")
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("empty json object", func(t *testing.T) {
		sessionJSON := `{}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("nested working_directory", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/projects/deep/nested/project","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "project" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "project")
		}
	})
}
