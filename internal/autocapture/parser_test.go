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

	t.Run("unparseable input falls back to global without error", func(t *testing.T) {
		// New contract: never hard-error. JSONL and malformed files must not
		// error out (that bug sent every JSONL session to "global").
		sessionJSON := `{"working_directory":}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("JSONL session resolves project from message bodies", func(t *testing.T) {
		jsonl := `{"role":"session_meta","source_file":"/home/rat/.claude/projects/-home-rat/x.jsonl"}
{"role":"user","content":"please look at /home/rat/mindbank/internal"}
{"role":"assistant","content":"editing /home/rat/mindbank/main.go"}`
		got, _ := ParseSessionForNamespace([]byte(jsonl))
		if got != "mindbank" {
			t.Errorf("JSONL namespace = %q, want %q", got, "mindbank")
		}
	})

	t.Run("claude source_file decodes project when no path in body", func(t *testing.T) {
		jsonl := `{"role":"session_meta","source_file":"/home/rat/.claude/projects/-home-rat-myproj/x.jsonl"}
{"role":"user","content":"hello"}`
		got, _ := ParseSessionForNamespace([]byte(jsonl))
		if got != "myproj" {
			t.Errorf("source_file namespace = %q, want %q", got, "myproj")
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

	t.Run("system_prompt with project path", func(t *testing.T) {
		sessionJSON := `{"system_prompt":"Project AGENTS.md for /home/rat/massagents. You are an AI agent.","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "massagents" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "massagents")
		}
	})

	t.Run("system_prompt with multiple projects picks most frequent", func(t *testing.T) {
		sessionJSON := `{"system_prompt":"Path: /home/rat/mindbank then /home/rat/mindbank again and /home/rat/other once.","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("user messages with project path", func(t *testing.T) {
		sessionJSON := `{"messages":[{"role":"user","content":"lets work on /home/rat/klixsor project"},{"role":"assistant","content":"ok"}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "klixsor" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "klixsor")
		}
	})

	t.Run("messages with punctuation after path", func(t *testing.T) {
		sessionJSON := `{"messages":[{"role":"user","content":"Review /home/rat/kataro. Then check /home/rat/kataro again."}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "kataro" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "kataro")
		}
	})

	t.Run("system_prompt overrides messages", func(t *testing.T) {
		sessionJSON := `{"system_prompt":"Project at /home/rat/mindbank","messages":[{"role":"user","content":"work on /home/rat/other"}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})

	t.Run("no paths falls back to global", func(t *testing.T) {
		sessionJSON := `{"system_prompt":"Just a normal prompt.","messages":[{"role":"user","content":"hello world"}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "global" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "global")
		}
	})

	t.Run("working_directory still takes priority over system_prompt", func(t *testing.T) {
		sessionJSON := `{"working_directory":"/home/rat/hermes","system_prompt":"Project at /home/rat/mindbank","messages":[]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "hermes" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "hermes")
		}
	})

	t.Run("trailing punctuation stripped", func(t *testing.T) {
		sessionJSON := `{"messages":[{"role":"user","content":"Check /home/rat/mindbank, /home/rat/kataro; /home/rat/klixsor."}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		// mindbank, kataro, klixsor each appear once — tiebreaker is alphabetical
		if got != "kataro" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "kataro")
		}
	})

	t.Run("common directories are filtered out", func(t *testing.T) {
		sessionJSON := `{"messages":[{"role":"user","content":"path: /home/rat/go and /home/rat/.config and /home/rat/mindbank"}]}`
		got, err := ParseSessionForNamespace([]byte(sessionJSON))
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
		if got != "mindbank" {
			t.Errorf("ParseSessionForNamespace() = %q, want %q", got, "mindbank")
		}
	})
}
