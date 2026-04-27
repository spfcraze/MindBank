package utils

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
