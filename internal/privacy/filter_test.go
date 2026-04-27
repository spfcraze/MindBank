package privacy

import (
	"strings"
	"testing"
)

func TestFilterOpenAIKey(t *testing.T) {
	input := "My key is sk-abcdefghijklmnopqrstuvwxyz123456789012345678"
	want := "My key is [REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterAWSKey(t *testing.T) {
	input := "AKIAIOSFODNN7EXAMPLE"
	want := "[REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterGitHubToken(t *testing.T) {
	input := "ghp_xxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxxx"
	want := "[REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterGitLabToken(t *testing.T) {
	input := "glpat-xxxxxxxxxxxxxxxxxxxx"
	want := "[REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterPassword(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"password: supersecret123", "[REDACTED]"},
		{"Password=supersecret123", "[REDACTED]"},
		{"pwd: supersecret123", "[REDACTED]"},
	}
	for _, c := range cases {
		got := Filter(c.input)
		if got != c.want {
			t.Errorf("Filter(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFilterSecret(t *testing.T) {
	cases := []struct {
		input string
		want  string
	}{
		{"secret: mysecretvalue", "[REDACTED]"},
		{"api_key=somekey123", "[REDACTED]"},
		{"token: abcdef123456", "[REDACTED]"},
	}
	for _, c := range cases {
		got := Filter(c.input)
		if got != c.want {
			t.Errorf("Filter(%q) = %q, want %q", c.input, got, c.want)
		}
	}
}

func TestFilterURLAuth(t *testing.T) {
	input := "https://user:pass@example.com/path"
	want := "[REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterJWT(t *testing.T) {
	input := "Authorization: Bearer eyJhbGciOiJIUzI1NiIsInR5cCI6IkpXVCJ9.eyJzdWIiOiIxMjM0NTY3ODkwIiwibmFtZSI6IkpvaG4gRG9lIiwiaWF0IjoxNTE2MjM5MDIyfQ.SflKxwRJSMeKKF2QT4fwpMeJf36POk6yJV_adQssw5c"
	want := "Authorization: Bearer [REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterMultipleSecrets(t *testing.T) {
	input := "AWS: AKIAIOSFODNN7EXAMPLE, OpenAI: sk-abcdefghijklmnopqrstuvwxyz123456789012345678"
	want := "AWS: [REDACTED], OpenAI: [REDACTED]"
	got := Filter(input)
	if got != want {
		t.Errorf("Filter(%q) = %q, want %q", input, got, want)
	}
}

func TestFilterNoSecrets(t *testing.T) {
	input := "This is a completely innocent string with no secrets."
	got := Filter(input)
	if got != input {
		t.Errorf("Filter(%q) = %q, want unchanged", input, got)
	}
}

func TestFilterNode(t *testing.T) {
	label := "Project with key sk-abcdefghijklmnopqrstuvwxyz123456789012345678"
	content := "password: supersecret123 and token: abcdef123456"
	summary := "Uses AKIAIOSFODNN7EXAMPLE"

	fl, fc, fs := FilterNode(label, content, summary)

	if !strings.Contains(fl, "[REDACTED]") {
		t.Errorf("FilterNode label did not redact: %q", fl)
	}
	if !strings.Contains(fc, "[REDACTED]") {
		t.Errorf("FilterNode content did not redact: %q", fc)
	}
	if !strings.Contains(fs, "[REDACTED]") {
		t.Errorf("FilterNode summary did not redact: %q", fs)
	}
}

func TestFilterNodeNoSecrets(t *testing.T) {
	label := "Clean label"
	content := "Clean content"
	summary := "Clean summary"

	fl, fc, fs := FilterNode(label, content, summary)

	if fl != label || fc != content || fs != summary {
		t.Error("FilterNode modified clean strings")
	}
}
