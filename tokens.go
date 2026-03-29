package otellix

// EstimateTokens provides a rough token count estimate from text length.
// This uses the common heuristic of ~4 characters per token for English text.
// It's intended for pre-call budget estimation, not exact accounting.
func EstimateTokens(text string) int64 {
	if len(text) == 0 {
		return 0
	}
	// ~4 characters per token is a reasonable average across GPT/Claude tokenizers.
	tokens := len(text) / 4
	if tokens == 0 {
		tokens = 1
	}
	return int64(tokens)
}

// EstimateTokensFromMessages estimates total input tokens across multiple messages.
func EstimateTokensFromMessages(messages []string) int64 {
	var total int64
	for _, msg := range messages {
		total += EstimateTokens(msg)
	}
	// Add overhead for message formatting (~4 tokens per message for role/separators).
	total += int64(len(messages) * 4)
	return total
}
