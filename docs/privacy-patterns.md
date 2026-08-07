# Privacy Filter Patterns

## Overview

The privacy filter (`internal/privacy/filter.go`) redacts sensitive data from node content before storage or API response. It runs automatically on all node label, content, and summary fields.

## Active Patterns

| Pattern | Regex | Example Match |
|---|---|---|
| openai_key | `sk-[a-zA-Z0-9]{20,}` | `sk-abc123...` |
| aws_key | `AKIA[0-9A-Z]{16}` | `AKIAIOSFODNN7EXAMPLE` |
| github_token | `gh[pousr]_[A-Za-z0-9_]{36,}` | `ghp_xxxxxxxx...` |
| gitlab_token | `glpat-[A-Za-z0-9\-]{20,}` | `glpat-xxxxxxxx...` |
| password | `(?i)(password\|passwd\|pwd)\s*[:=]\s*[^\s]{4,}` | `password: secret123` |
| secret | `(?i)(secret\|api[_-]?key\|token)\s*[:=]\s*[^\s]{4,}` | `secret: myvalue` |
| url_auth | `[a-zA-Z][a-zA-Z0-9+.-]*://[^\s:@]+:[^\s:@]+@[^\s]+` | `https://user:pass@host` |
| jwt | `eyJ[a-zA-Z0-9_-]*\.eyJ[a-zA-Z0-9_-]*\.[a-zA-Z0-9_-]*` | `eyJhbG...` |
| ssh_key | `-----BEGIN (RSA\|DSA\|EC\|OPENSSH) PRIVATE KEY-----` | SSH PEM header |
| db_url | `(postgres\|mysql\|mongodb\|redis)://[^\s]+` | `postgres://user:pass@host/db` |
| slack_token | `xox[baprs]-[a-zA-Z0-9-]+` | `xoxb-xxxxxxxx...` |
| aws_secret | Context-aware (see below) | `AKIA...` + 40-char base64 nearby |
| stripe_key | `(sk\|pk)_(live\|test)_[a-zA-Z0-9]{24,}` | `sk_live_xxxxxxxx...` |
| discord_webhook | `https://discord\.com/api/webhooks/[0-9]+/[a-zA-Z0-9_-]+` | Webhook URL |
| private_key | `(?i)(private[_-]?key\|privkey)\s*[:=]\s*[^\s]{10,}` | `private_key: xxx...` |

## AWS Secret Pattern — Context-Aware

The `aws_secret` pattern is **constrained** to prevent false positives:

- Must either follow an AWS access key (`AKIA...`) within 50 chars, OR
- Must appear near the word "secret" within 20 chars

This prevents redaction of random 40-char base64 strings (SHA-1 hashes, data URIs, etc.).

## Adding New Patterns

1. Add compiled regex to `filter.go`.
2. Add to `DefaultPatterns` map.
3. Add test to `filter_test.go` covering:
   - Positive match (secret is redacted)
   - Negative match (innocent text is NOT redacted)
4. Run `go test ./internal/privacy/`.

## API

```go
// Filter redacts all patterns from text
redacted := privacy.Filter("my key is sk-abc123")
// → "my key is [REDACTED]"

// FilterNode redacts label, content, summary
rl, rc, rs := privacy.FilterNode(label, content, summary)
```
