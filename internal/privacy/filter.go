package privacy

import (
	"regexp"
)

var (
	// openai_key matches OpenAI API keys (sk-...)
	openaiKeyPattern = regexp.MustCompile(`sk-[a-zA-Z0-9]{20,}`)

	// aws_key matches AWS access key IDs (AKIA...)
	awsKeyPattern = regexp.MustCompile(`AKIA[0-9A-Z]{16}`)

	// github_token matches GitHub personal access tokens (ghp_..., gho_..., etc.)
	githubTokenPattern = regexp.MustCompile(`gh[pousr]_[A-Za-z0-9_]{36,}`)

	// gitlab_token matches GitLab personal access tokens (glpat-...)
	gitlabTokenPattern = regexp.MustCompile(`glpat-[A-Za-z0-9\-]{20,}`)

	// password matches common password patterns
	passwordPattern = regexp.MustCompile(`(?i)(password|passwd|pwd)\s*[:=]\s*[^\s]{4,}`)

	// secret matches common secret patterns
	secretPattern = regexp.MustCompile(`(?i)(secret|api[_-]?key|token)\s*[:=]\s*[^\s]{4,}`)

	// url_auth matches URLs with embedded credentials
	urlAuthPattern = regexp.MustCompile(`[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:@]+:[^\s:@]+@[^\s]+`)

	// jwt matches JSON Web Tokens (3 base64url segments separated by dots)
	jwtPattern = regexp.MustCompile(`eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*`)
)

// DefaultPatterns is the list of all default regex patterns used for filtering.
// Each entry maps a friendly name to its compiled regex.
var DefaultPatterns = map[string]*regexp.Regexp{
	"openai_key":  openaiKeyPattern,
	"aws_key":     awsKeyPattern,
	"github_token": githubTokenPattern,
	"gitlab_token": gitlabTokenPattern,
	"password":    passwordPattern,
	"secret":      secretPattern,
	"url_auth":    urlAuthPattern,
	"jwt":         jwtPattern,
}

// Filter strips all default patterns from the given text, replacing matches
// with "[REDACTED]".
func Filter(text string) string {
	for _, re := range DefaultPatterns {
		text = re.ReplaceAllString(text, "[REDACTED]")
	}
	return text
}

// FilterNode filters label, content, and summary fields, returning the
// redacted versions.
func FilterNode(label, content, summary string) (string, string, string) {
	return Filter(label), Filter(content), Filter(summary)
}
