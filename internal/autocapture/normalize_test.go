package autocapture

import (
	"testing"
)

func TestNormalizePath_StripWorkerSuffixes(t *testing.T) {
	tests := []struct {
		path     string
		expected string
	}{
		// Worker suffixes
		{"/home/rat/hermes-city-worker-121", "/home/rat/hermes-city"},
		{"/home/rat/mindbank-worker-20", "/home/rat/mindbank"},
		{"/home/rat/mindbank1-worker-117", "/home/rat/mindbank1"},
		{"/home/rat/ultraclaude-worker-150", "/home/rat/ultraclaude"},
		{"/home/rat/test-website-team-worker-119", "/home/rat/test-website-team"},
		{"/home/rat/testmode-worker-123", "/home/rat/testmode"},
		
		// Multiple worker suffixes (edge case)
		{"/home/rat/hermes-city-worker-121-worker-122", "/home/rat/hermes-city"},
		
		// No worker suffix — should pass through
		{"/home/rat/mindbank", "/home/rat/mindbank"},
		{"/home/rat/projects/klixsor", "/home/rat/projects/klixsor"},
		
		// Trailing slash + worker
		{"/home/rat/mindbank-worker-20/", "/home/rat/mindbank"},
		
		// Root paths
		{"/", "/"},
		{"", ""},
		
		// Hidden folders (should not be stripped)
		{"/home/rat/.hidden-worker-1", "/home/rat/.hidden"},
	}

	for _, tt := range tests {
		t.Run(tt.path, func(t *testing.T) {
			got := NormalizePath(tt.path)
			if got != tt.expected {
				t.Errorf("NormalizePath(%q) = %q, want %q", tt.path, got, tt.expected)
			}
		})
	}
}

func TestDeriveNamespaceFromPath_WithNormalization(t *testing.T) {
	// After normalization, worker paths should resolve to project name
	tests := []struct {
		path     string
		expected string
	}{
		{"/home/rat/hermes-city-worker-121", "hermes-city"},
		{"/home/rat/mindbank-worker-20", "mindbank"},
		{"/home/rat/mindbank1-worker-117", "mindbank1"},
		{"/home/rat/ultraclaude-worker-150", "ultraclaude"},
		{"/home/rat/test-website-team-worker-119", "test-website-team"},
		{"/home/rat/testmode-worker-123", "testmode"},
		
		// Clean paths (no normalization needed)
		{"/home/rat/mindbank", "mindbank"},
		{"/home/rat/projects/klixsor", "klixsor"},
		{"/home/rat", "rat"},
		
		// Edge cases
		{"", "global"},
		{"/", "global"},
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

func TestParseSessionForNamespace_WithWorkerPaths(t *testing.T) {
	t.Run("worker path in working_directory", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/mindbank-worker-20","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("team worker path in cwd", func(t *testing.T) {
		sessionJSON := `{"cwd":"/home/rat/test-website-team-worker-120","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "test-website-team" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "test-website-team")
		}
	})

	t.Run("nested worker path", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/projects/deep/mindbank-worker-99","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})
}
