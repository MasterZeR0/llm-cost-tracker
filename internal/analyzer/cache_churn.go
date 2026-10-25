package analyzer

import (
	"fmt"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

// DetectCacheChurn inspects turns for sudden collapses in prompt caching hit rates on large contexts.
// It returns detected WasteFindings, total wasted tokens, and total wasted USD.
func DetectCacheChurn(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64) {
	if len(turns) < 2 {
		return nil, 0, 0
	}

	dropThreshold := cfg.CacheChurnDropThreshold
	if dropThreshold <= 0 {
		dropThreshold = 0.40
	}

	engine := pricing.NewEngine(nil)

	var findings []WasteFinding
	var totalWastedToks int64
	var totalWastedUSD float64

	var cumulativeCacheRead int64
	var cumulativePrompt int64

	for i, turn := range turns {
		turnPrompt := turn.InputTokens + turn.CacheReadTokens
		if turnPrompt == 0 {
			continue
		}

		turnHitRate := float64(turn.CacheReadTokens) / float64(turnPrompt)

		// Check if baseline was established by earlier turns
		if i > 0 && cumulativePrompt > 0 {
			baselineHitRate := float64(cumulativeCacheRead) / float64(cumulativePrompt)

			// Baseline established if hit rate >= 50%
			if baselineHitRate >= 0.50 {
				rateDrop := baselineHitRate - turnHitRate

				// Trigger when turn has substantial input tokens (>= 20k or total prompt >= 20k)
				// and hit rate dropped sharply (drop >= threshold, or dropped below 25% with significant drop)
				if (turn.InputTokens >= 20000 || turnPrompt >= 20000) && (rateDrop >= dropThreshold || (turnHitRate < 0.25 && rateDrop >= 0.25)) {
					expectedHitTokens := int64(float64(turnPrompt) * baselineHitRate)
					churnedTokens := expectedHitTokens - turn.CacheReadTokens
					if churnedTokens < 0 {
						churnedTokens = 0
					}

					if churnedTokens > 0 {
						turnUSD := computeCacheChurnCost(turn, churnedTokens, engine)

						severity := SeverityMedium
						if churnedTokens >= 50000 || turnUSD >= 1.0 {
							severity = SeverityCritical
						} else if churnedTokens >= 20000 || turnUSD >= 0.25 || rateDrop >= 0.40 {
							severity = SeverityHigh
						}

						step := turn.StepIndex
						if step == 0 {
							step = int64(i + 1)
						}

						findings = append(findings, WasteFinding{
							Category:       WasteCacheChurn,
							Severity:       severity,
							TurnIndex:      step,
							WastedTokens:   churnedTokens,
							WastedUSD:      turnUSD,
							Explanation:    fmt.Sprintf("Cache hit rate collapsed from %.1f%% to %.1f%% on turn %d with %d input tokens (lost %d cached tokens).", baselineHitRate*100.0, turnHitRate*100.0, step, turn.InputTokens, churnedTokens),
							Recommendation: "Preserve prompt prefix stability by keeping instructions and system prompts constant across turns.",
						})

						totalWastedToks += churnedTokens
						totalWastedUSD += turnUSD
					}
				}
			}
		}

		cumulativeCacheRead += turn.CacheReadTokens
		cumulativePrompt += turnPrompt
	}

	return findings, totalWastedToks, totalWastedUSD
}

func computeCacheChurnCost(turn store.Request, churnedTokens int64, engine *pricing.Engine) float64 {
	if churnedTokens <= 0 {
		return 0.0
	}
	if turn.CostUSD != nil && *turn.CostUSD <= 0.0 {
		return 0.0
	}
	if engine != nil && turn.Model != "" {
		missCost, ok1 := engine.CalculateCost(turn.Model, churnedTokens, 0, 0, 0)
		hitCost, ok2 := engine.CalculateCost(turn.Model, 0, 0, churnedTokens, 0)
		if ok1 && ok2 && missCost != nil && hitCost != nil {
			delta := *missCost - *hitCost
			if delta <= 0 {
				return 0.0
			}
			if turn.CostUSD != nil && *turn.CostUSD > 0 && delta > *turn.CostUSD {
				return *turn.CostUSD
			}
			return delta
		}
	}
	// Fallback using turn cost ratio if available
	if turn.CostUSD != nil && *turn.CostUSD > 0 && turn.TotalTokens > 0 {
		effectiveRate := *turn.CostUSD / float64(turn.TotalTokens)
		cost := float64(churnedTokens) * effectiveRate * 0.90
		if cost > *turn.CostUSD {
			cost = *turn.CostUSD
		}
		return cost
	}
	// Standard fallback: delta of $2.70 per million tokens ($3.00 input - $0.30 cache read)
	return float64(churnedTokens) * 2.70 / 1_000_000.0
}
