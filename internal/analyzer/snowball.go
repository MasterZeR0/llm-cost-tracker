package analyzer

import (
	"fmt"
	"sort"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

// DetectSnowball evaluates turns for context snowballing and extreme context growth.
// It returns detected WasteFindings, total wasted tokens, and total wasted USD.
func DetectSnowball(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64) {
	n := len(turns)
	if n == 0 {
		return nil, 0, 0
	}

	snowballThresh := cfg.SnowballTokenThreshold
	if snowballThresh <= 0 {
		snowballThresh = 80000
	}
	severeThresh := cfg.SevereTurnTokenThreshold
	if severeThresh <= 0 {
		severeThresh = 120000
	}

	engine := pricing.NewEngine(nil)

	var findings []WasteFinding
	var totalWastedToks int64
	var totalWastedUSD float64

	// Determine early turns and median context size
	var earlyMedian int64
	lateCount := n / 4
	if lateCount < 2 {
		lateCount = 2
	}
	lateStartIndex := n - lateCount

	if n >= 8 {
		earlyTurns := turns[:lateStartIndex]
		earlyMedian = calculateMedianTokens(earlyTurns)
	}

	// Calculate total session cost
	var totalSessionCost float64
	for _, t := range turns {
		if t.CostUSD != nil {
			totalSessionCost += *t.CostUSD
		} else if engine != nil && t.Model != "" {
			if cost, ok := engine.CalculateCost(t.Model, t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens); ok && cost != nil {
				totalSessionCost += *cost
			}
		}
	}

	snowballTriggered := false

	// Check late-turn snowball if total turns >= 8
	if n >= 8 {
		lateTurns := turns[lateStartIndex:]

		var lateCost float64
		var maxLateTokens int64
		for _, t := range lateTurns {
			if t.CostUSD != nil {
				lateCost += *t.CostUSD
			} else if engine != nil && t.Model != "" {
				if cost, ok := engine.CalculateCost(t.Model, t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens); ok && cost != nil {
					lateCost += *cost
				}
			}
			if t.InputTokens > maxLateTokens {
				maxLateTokens = t.InputTokens
			}
		}

		lateCostRatio := 0.0
		if totalSessionCost > 0 {
			lateCostRatio = lateCost / totalSessionCost
		} else {
			// Fallback to token ratio if costs are unmetered
			var totalInput, lateInput int64
			for _, t := range turns {
				totalInput += t.InputTokens
			}
			for _, t := range lateTurns {
				lateInput += t.InputTokens
			}
			if totalInput > 0 {
				lateCostRatio = float64(lateInput) / float64(totalInput)
			}
		}

		// Trigger if late turns account for >= 60% of cost and input tokens exceed SnowballTokenThreshold
		if lateCostRatio >= 0.60 && maxLateTokens >= snowballThresh {
			snowballTriggered = true

			var lateWastedTokens int64
			var lateWastedCost float64

			for _, t := range lateTurns {
				excess := t.InputTokens - earlyMedian
				if excess > 0 {
					lateWastedTokens += excess
					lateWastedCost += computeTurnInputCost(t, excess, engine)
				}
			}

			severity := SeverityHigh
			if maxLateTokens >= severeThresh {
				severity = SeverityCritical
			}

			firstLateStep := lateTurns[0].StepIndex
			if firstLateStep == 0 {
				firstLateStep = int64(lateStartIndex + 1)
			}

			findings = append(findings, WasteFinding{
				Category:       WasteContextSnowball,
				Severity:       severity,
				TurnIndex:      firstLateStep,
				WastedTokens:   lateWastedTokens,
				WastedUSD:      lateWastedCost,
				Explanation:    fmt.Sprintf("Late turns (last 25%%) consumed %.1f%% of session cost with context peaking at %d tokens (early median: %d tokens).", lateCostRatio*100.0, maxLateTokens, earlyMedian),
				Recommendation: "Compact context earlier or split long-running tasks into focused subagent sessions using /compact or session branching.",
			})

			totalWastedToks += lateWastedTokens
			totalWastedUSD += lateWastedCost
		}
	}

	// Check any individual turns exceeding SevereTurnTokenThreshold not already captured
	for idx, t := range turns {
		if snowballTriggered && idx >= lateStartIndex {
			// Already covered under late turns snowball
			continue
		}

		if t.InputTokens >= severeThresh {
			excess := t.InputTokens - earlyMedian
			if earlyMedian <= 0 || excess <= 0 {
				excess = t.InputTokens - snowballThresh
			}
			if excess <= 0 {
				excess = t.InputTokens - severeThresh
			}
			if excess <= 0 {
				excess = t.InputTokens / 2
			}

			turnUSD := computeTurnInputCost(t, excess, engine)
			step := t.StepIndex
			if step == 0 {
				step = int64(idx + 1)
			}

			findings = append(findings, WasteFinding{
				Category:       WasteContextSnowball,
				Severity:       SeverityCritical,
				TurnIndex:      step,
				WastedTokens:   excess,
				WastedUSD:      turnUSD,
				Explanation:    fmt.Sprintf("Turn %d context reached %d tokens, exceeding severe threshold (%d tokens).", step, t.InputTokens, severeThresh),
				Recommendation: "Use context compaction (/compact) or offload large files to scratchpads to prevent context explosion.",
			})

			totalWastedToks += excess
			totalWastedUSD += turnUSD
		}
	}

	return findings, totalWastedToks, totalWastedUSD
}

func calculateMedianTokens(turns []store.Request) int64 {
	if len(turns) == 0 {
		return 0
	}
	vals := make([]int64, len(turns))
	for i, t := range turns {
		vals[i] = t.InputTokens
	}
	sort.Slice(vals, func(i, j int) bool {
		return vals[i] < vals[j]
	})
	mid := len(vals) / 2
	if len(vals)%2 != 0 {
		return vals[mid]
	}
	return (vals[mid-1] + vals[mid]) / 2
}

func computeTurnInputCost(turn store.Request, tokens int64, engine *pricing.Engine) float64 {
	if tokens <= 0 {
		return 0.0
	}
	if turn.CostUSD != nil && *turn.CostUSD <= 0.0 {
		return 0.0
	}
	if engine != nil && turn.Model != "" {
		if cost, ok := engine.CalculateCost(turn.Model, tokens, 0, 0, 0); ok && cost != nil {
			if *cost <= 0.0 {
				return 0.0
			}
			if turn.CostUSD != nil && *turn.CostUSD > 0 && *cost > *turn.CostUSD {
				return *turn.CostUSD
			}
			return *cost
		}
	}
	// Fallback using turn CostUSD ratio
	if turn.CostUSD != nil && *turn.CostUSD > 0 {
		if turn.InputTokens > 0 {
			cost := *turn.CostUSD * (float64(tokens) / float64(turn.InputTokens))
			if cost > *turn.CostUSD {
				cost = *turn.CostUSD
			}
			return cost
		}
		if turn.TotalTokens > 0 {
			cost := *turn.CostUSD * (float64(tokens) / float64(turn.TotalTokens))
			if cost > *turn.CostUSD {
				cost = *turn.CostUSD
			}
			return cost
		}
	}
	// Standard fallback rate: $3.00 per million tokens
	return float64(tokens) * 3.00 / 1_000_000.0
}
