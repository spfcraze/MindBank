package utils

import "unicode/utf8"

// TruncateUTF8 shortens s to at most maxBytes without splitting a multi-byte
// rune; a plain s[:n] cut can leave an invalid byte that JSON encoders turn
// into U+FFFD, handing agents mangled memory content.
func TruncateUTF8(s string, maxBytes int) string {
	if len(s) <= maxBytes {
		return s
	}
	cut := maxBytes
	for cut > 0 && !utf8.RuneStart(s[cut]) {
		cut--
	}
	return s[:cut]
}

// EstimateTokens returns a rough token count for a text string.
// Uses the heuristic: 4 characters ≈ 1 token (English average).
func EstimateTokens(text string) int {
	if text == "" {
		return 0
	}
	return len(text) / 4
}

// EstimateNodeTokens estimates the total token count for a node's text fields.
// Caps content contribution to avoid over-counting large bodies.
func EstimateNodeTokens(label, summary, content string) int {
	total := EstimateTokens(label)
	total += EstimateTokens(summary)
	// Cap content contribution at 500 tokens worth
	contentTokens := EstimateTokens(content)
	if contentTokens > 500 {
		contentTokens = 500
	}
	total += contentTokens
	return total
}
