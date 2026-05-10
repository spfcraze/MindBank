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

	// ssh_key matches SSH private key headers
	sshKeyPattern = regexp.MustCompile(`-----BEGIN (RSA|DSA|EC|OPENSSH) PRIVATE KEY-----`)

	// db_url matches database connection strings with credentials
	dbURLPattern = regexp.MustCompile(`(postgres|mysql|mongodb|redis)://[^\s]+`)

	// slack_token matches Slack tokens (xoxb-, xoxa-, xoxr-, etc.)
	slackTokenPattern = regexp.MustCompile(`xox[baprs]-[a-zA-Z0-9-]+`)

	// aws_secret matches AWS secret access keys (40 char base64-like)
	// Constrained: must follow an AWS access key or appear near "secret" context
	awsSecretPattern = regexp.MustCompile(`(?i)(AKIA[0-9A-Z]{16}[^\n]{0,50}[A-Za-z0-9/+=]{40}|secret[^\n]{0,20}[A-Za-z0-9/+=]{40})`)

	// stripe_key matches Stripe API keys (sk_live_, sk_test_, pk_live_, pk_test_)
	stripeKeyPattern = regexp.MustCompile(`(sk|pk)_(live|test)_[a-zA-Z0-9]{24,}`)

	// discord_webhook matches Discord webhook URLs
	discordWebhookPattern = regexp.MustCompile(`https://discord\.com/api/webhooks/[0-9]+/[a-zA-Z0-9_-]+`)

	// private_key matches generic private key patterns
	privateKeyPattern = regexp.MustCompile(`(?i)(private[_-]?key|privkey)\s*[:=]\s*[^\s]{10,}`)
)

// DefaultPatterns is the list of all default regex patterns used for filtering.
// Each entry maps a friendly name to its compiled regex.
var DefaultPatterns = map[string]*regexp.Regexp{
	"openai_key":       openaiKeyPattern,
	"aws_key":          awsKeyPattern,
	"github_token":     githubTokenPattern,
	"gitlab_token":     gitlabTokenPattern,
	"password":         passwordPattern,
	"secret":           secretPattern,
	"url_auth":         urlAuthPattern,
	"jwt":              jwtPattern,
	"ssh_key":          sshKeyPattern,
	"db_url":           dbURLPattern,
	"slack_token":      slackTokenPattern,
	"aws_secret":       awsSecretPattern,
	"stripe_key":       stripeKeyPattern,
	"discord_webhook":  discordWebhookPattern,
	"private_key":      privateKeyPattern,
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
