package translatorbot

// EstimateTranslationTokens is a character/4 heuristic used for history
// generation cuts, topic-summary input caps, prompt-cache thresholds, and
// rate-limit admission. It is not provider-reported usage.
func EstimateTranslationTokens(prompt, response string) int {
	total := len(prompt) + len(response)
	if total == 0 {
		return 0
	}
	tokens := total / 4
	if tokens < 1 {
		return 1
	}
	return tokens
}
