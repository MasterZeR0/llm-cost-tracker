package analyzer

import (
	"math"
	"sort"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

// AnalyzeSession runs all waste heuristics on a session and calculates overall metrics,
// waste trajectory, health score, and actionable findings.
func AnalyzeSession(detail store.SessionDetail, cfg Config) SessionWasteAnalysis {
	sessionID := detail.Summary.SessionID
	tool := detail.Summary.Tool
	if sessionID == "" && len(detail.Turns) > 0 {
		sessionID = detail.Turns[0].SessionID
	}
	if tool == "" && len(detail.Turns) > 0 {
		tool = detail.Turns[0].Tool
	}

	engine := pricing.NewEngine(nil)

	totalTokens := detail.Summary.TotalTokens
	totalCost := detail.Summary.TotalCost

	if totalTokens == 0 {
		for _, t := range detail.Turns {
			if t.TotalTokens > 0 {
				totalTokens += t.TotalTokens
			} else {
				totalTokens += t.InputTokens + t.OutputTokens + t.CacheReadTokens
			}
		}
	}

	if totalCost == 0 {
		for _, t := range detail.Turns {
			if t.CostUSD != nil {
				totalCost += *t.CostUSD
			} else if engine != nil && t.Model != "" {
				if cost, ok := engine.CalculateCost(t.Model, t.InputTokens, t.OutputTokens, t.CacheReadTokens, t.CacheWriteTokens); ok && cost != nil {
					totalCost += *cost
				}
			}
		}
	}

	// Build trajectory for all turns
	trajectory := make([]TurnMetric, len(detail.Turns))
	for i, turn := range detail.Turns {
		step := turn.StepIndex
		if step == 0 {
			step = int64(i + 1)
		}
		cost := 0.0
		if turn.CostUSD != nil {
			cost = *turn.CostUSD
		} else if engine != nil && turn.Model != "" {
			if c, ok := engine.CalculateCost(turn.Model, turn.InputTokens, turn.OutputTokens, turn.CacheReadTokens, turn.CacheWriteTokens); ok && c != nil {
				cost = *c
			}
		}

		trajectory[i] = TurnMetric{
			StepIndex:       step,
			InputTokens:     turn.InputTokens,
			OutputTokens:    turn.OutputTokens,
			CacheReadTokens: turn.CacheReadTokens,
			CostUSD:         cost,
			HasToolError:    isFailureTurn(turn),
			IsWaste:         false,
		}
	}

	// Edge cases: 0 turns, 1 turn, or disabled analyzer -> 0 waste, 100 health score
	if !cfg.Enabled || len(detail.Turns) <= 1 {
		return SessionWasteAnalysis{
			SessionID:       sessionID,
			Tool:            tool,
			TotalTokens:     totalTokens,
			TotalCost:       totalCost,
			WastedTokens:    0,
			WastedUSD:       0,
			WastePercentage: 0,
			HealthScore:     100,
			Findings:        []WasteFinding{},
			Trajectory:      trajectory,
		}
	}

	findings := make([]WasteFinding, 0)
	var totalWastedTokens int64
	var totalWastedUSD float64

	// 1. Context Snowball detector
	snowballFindings, snowToks, snowCost := DetectSnowball(detail.Turns, cfg)
	findings = append(findings, snowballFindings...)
	totalWastedTokens += snowToks
	totalWastedUSD += snowCost

	// 2. Cache Churn detector
	churnFindings, churnToks, churnCost := DetectCacheChurn(detail.Turns, cfg)
	findings = append(findings, churnFindings...)
	totalWastedTokens += churnToks
	totalWastedUSD += churnCost

	// 3. Error Loop detector
	errorFindings, errToks, errCost := DetectErrorLoops(detail.Turns, cfg)
	findings = append(findings, errorFindings...)
	totalWastedTokens += errToks
	totalWastedUSD += errCost

	// Mark trajectory steps that contributed to waste
	n := len(detail.Turns)
	severeThresh := cfg.SevereTurnTokenThreshold
	if severeThresh <= 0 {
		severeThresh = 120000
	}

	if len(snowballFindings) > 0 {
		lateCount := n / 4
		if lateCount < 2 {
			lateCount = 2
		}
		if n >= 8 {
			earlyMedian := calculateMedianTokens(detail.Turns[:n-lateCount])
			for i := n - lateCount; i < n; i++ {
				if detail.Turns[i].InputTokens > earlyMedian {
					trajectory[i].IsWaste = true
				}
			}
		}
	}

	for i, turn := range detail.Turns {
		if turn.InputTokens >= severeThresh {
			trajectory[i].IsWaste = true
		}
	}

	// Mark repeated failure turns in error loops
	var errorStreak []int
	for i, turn := range detail.Turns {
		if isFailureTurn(turn) {
			errorStreak = append(errorStreak, i)
		} else {
			if len(errorStreak) >= 2 {
				for _, idx := range errorStreak[1:] {
					trajectory[idx].IsWaste = true
				}
			}
			errorStreak = nil
		}
	}
	if len(errorStreak) >= 2 {
		for _, idx := range errorStreak[1:] {
			trajectory[idx].IsWaste = true
		}
	}

	// Mark steps referenced in findings
	for _, f := range findings {
		for i := range trajectory {
			if trajectory[i].StepIndex == f.TurnIndex {
				trajectory[i].IsWaste = true
			}
		}
	}

	// Cap wasted values at session totals
	if totalCost > 0 && totalWastedUSD > totalCost {
		totalWastedUSD = totalCost
	}
	if totalTokens > 0 && totalWastedTokens > totalTokens {
		totalWastedTokens = totalTokens
	}

	// Calculate waste percentage
	wastePercentage := 0.0
	if totalCost > 0 {
		wastePercentage = (totalWastedUSD / totalCost) * 100.0
	} else if totalTokens > 0 {
		wastePercentage = (float64(totalWastedTokens) / float64(totalTokens)) * 100.0
	}
	if wastePercentage > 100.0 {
		wastePercentage = 100.0
	}

	// Count findings by severity
	criticalCount := 0
	highCount := 0
	for _, f := range findings {
		switch f.Severity {
		case SeverityCritical:
			criticalCount++
		case SeverityHigh:
			highCount++
		}
	}

	// Health score formula: max(0, min(100, int(100 - (wastePercentage * 1.25) - (criticalCount * 20) - (highCount * 10))))
	rawHealth := 100.0 - (wastePercentage * 1.25) - float64(criticalCount*20) - float64(highCount*10)
	healthScore := int(math.Round(rawHealth))
	if healthScore < 0 {
		healthScore = 0
	} else if healthScore > 100 {
		healthScore = 100
	}

	return SessionWasteAnalysis{
		SessionID:       sessionID,
		Tool:            tool,
		TotalTokens:     totalTokens,
		TotalCost:       totalCost,
		WastedTokens:    totalWastedTokens,
		WastedUSD:       totalWastedUSD,
		WastePercentage: wastePercentage,
		HealthScore:     healthScore,
		Findings:        findings,
		Trajectory:      trajectory,
	}
}

// AnalyzePeriod analyzes a collection of sessions across a period, calculating summary aggregates,
// category totals, average health score, and ranked top waste sessions.
func AnalyzePeriod(sessions []store.SessionDetail, period string, cfg Config) PeriodWasteSummary {
	summary := PeriodWasteSummary{
		Period:        period,
		TotalSessions: int64(len(sessions)),
		CategoryTotals: map[WasteCategory]float64{
			WasteContextSnowball: 0.0,
			WasteCacheChurn:      0.0,
			WasteErrorLoop:       0.0,
			WasteRepetitiveRead:  0.0,
		},
		TopWasteSessions: []SessionWasteAnalysis{},
	}

	if len(sessions) == 0 {
		summary.AvgHealthScore = 100.0
		return summary
	}

	var totalHealth float64
	for _, sess := range sessions {
		analysis := AnalyzeSession(sess, cfg)
		totalHealth += float64(analysis.HealthScore)
		summary.TotalWastedUSD += analysis.WastedUSD
		summary.TotalWastedToks += analysis.WastedTokens

		for _, f := range analysis.Findings {
			summary.CategoryTotals[f.Category] += f.WastedUSD
		}

		if analysis.WastedUSD > 0 || analysis.HealthScore < 90 || len(analysis.Findings) > 0 {
			summary.WastedSessions++
			summary.TopWasteSessions = append(summary.TopWasteSessions, analysis)
		}
	}

	summary.AvgHealthScore = totalHealth / float64(len(sessions))

	// Sort TopWasteSessions by WastedUSD descending, then WastePercentage descending, then HealthScore ascending
	sort.Slice(summary.TopWasteSessions, func(i, j int) bool {
		if summary.TopWasteSessions[i].WastedUSD != summary.TopWasteSessions[j].WastedUSD {
			return summary.TopWasteSessions[i].WastedUSD > summary.TopWasteSessions[j].WastedUSD
		}
		if summary.TopWasteSessions[i].WastePercentage != summary.TopWasteSessions[j].WastePercentage {
			return summary.TopWasteSessions[i].WastePercentage > summary.TopWasteSessions[j].WastePercentage
		}
		return summary.TopWasteSessions[i].HealthScore < summary.TopWasteSessions[j].HealthScore
	})

	return summary
}
