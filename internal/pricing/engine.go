package pricing

import (
	"path/filepath"
	"sort"
	"strings"
)

type Engine struct {
	prices   map[string]ModelPrice
	patterns []string
}

func NewEngine(customPrices map[string]ModelPrice) *Engine {
	merged := make(map[string]ModelPrice)
	for k, v := range DefaultModelPrices {
		merged[k] = v
	}
	for k, v := range customPrices {
		merged[k] = v
	}

	patterns := make([]string, 0, len(merged))
	for k := range merged {
		patterns = append(patterns, k)
	}
	// Sort patterns by length descending, then alphabetically for deterministic matching
	// This ensures more specific patterns (e.g. gpt-4o-mini) are checked before shorter ones (e.g. gpt-4o)
	sort.Slice(patterns, func(i, j int) bool {
		if len(patterns[i]) != len(patterns[j]) {
			return len(patterns[i]) > len(patterns[j])
		}
		return patterns[i] < patterns[j]
	})

	return &Engine{
		prices:   merged,
		patterns: patterns,
	}
}

var knownProviderPrefixes = []string{
	"anthropic/",
	"openai/",
	"google/",
	"openrouter/",
	"meta-llama/",
	"mistralai/",
	"cohere/",
	"bedrock/",
	"azure/",
	"together/",
	"deepseek/",
	"groq/",
	"amazon/",
}

func getModelCandidates(modelLower string) []string {
	candidates := []string{modelLower}
	for _, p := range knownProviderPrefixes {
		if strings.HasPrefix(modelLower, p) {
			trimmed := strings.TrimPrefix(modelLower, p)
			if trimmed != "" {
				candidates = append(candidates, trimmed)
			}
		}
	}
	if idx := strings.LastIndex(modelLower, "/"); idx >= 0 && idx < len(modelLower)-1 {
		base := modelLower[idx+1:]
		var exists bool
		for _, c := range candidates {
			if c == base {
				exists = true
				break
			}
		}
		if !exists {
			candidates = append(candidates, base)
		}
	}
	return candidates
}

func matchPattern(pattern, candidate string) bool {
	if match, err := filepath.Match(pattern, candidate); err == nil && match {
		return true
	}
	if !strings.Contains(pattern, "*") && strings.Contains(candidate, pattern) {
		return true
	}
	return false
}

func (e *Engine) CalculateCost(model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) (*float64, bool) {
	if model == "" {
		return nil, false
	}

	patterns := e.patterns
	if len(patterns) == 0 && len(e.prices) > 0 {
		for k := range e.prices {
			patterns = append(patterns, k)
		}
	}

	modelLower := strings.ToLower(model)
	candidates := getModelCandidates(modelLower)

	var price ModelPrice
	var matched bool

	for _, pattern := range patterns {
		patLower := strings.ToLower(pattern)
		for _, candidate := range candidates {
			if matchPattern(patLower, candidate) {
				price = e.prices[pattern]
				matched = true
				break
			}
		}
		if matched {
			break
		}
		// Also if pattern has a provider prefix and candidate didn't match directly, try stripped pattern against modelLower
		if strings.Contains(patLower, "/") {
			patCandidates := getModelCandidates(patLower)
			for _, patCand := range patCandidates[1:] {
				if matchPattern(patCand, modelLower) {
					price = e.prices[pattern]
					matched = true
					break
				}
			}
			if matched {
				break
			}
		}
	}

	if !matched {
		return nil, false
	}

	cost := (float64(inputTokens)/1_000_000.0)*price.InputPerMTok +
		(float64(outputTokens)/1_000_000.0)*price.OutputPerMTok +
		(float64(cacheReadTokens)/1_000_000.0)*price.CacheReadPerMTok +
		(float64(cacheWriteTokens)/1_000_000.0)*price.CacheWritePerMTok

	return &cost, true
}
