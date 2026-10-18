package tokenestimator

import (
	"math"
	"strings"
)

// EstimateTokens calculates approximate token counts for prompt and completion
// based on standard subword token ratios (~4.0 chars/token for prompt, ~3.8 for completion).
func EstimateTokens(promptText, responseText string) (int64, int64) {
	promptLen := len(strings.TrimSpace(promptText))
	respLen := len(strings.TrimSpace(responseText))

	var inputTokens int64
	if promptLen > 0 {
		inputTokens = int64(math.Ceil(float64(promptLen) / 4.0))
		if inputTokens < 1 {
			inputTokens = 1
		}
	}

	var outputTokens int64
	if respLen > 0 {
		outputTokens = int64(math.Ceil(float64(respLen) / 3.8))
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	return inputTokens, outputTokens
}
