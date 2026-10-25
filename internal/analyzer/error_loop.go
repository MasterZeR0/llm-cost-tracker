package analyzer

import (
	"fmt"
	"strings"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func isFailureTurn(turn store.Request) bool {
	if turn.ToolErrorCount > 0 {
		return true
	}
	st := strings.ToLower(strings.TrimSpace(turn.Status))
	return st == "error" || st == "failed" || st == "failure"
}

// DetectErrorLoops scans turns for consecutive tool or execution failures.
// It returns detected WasteFindings, total wasted tokens, and total wasted USD across repeated failures.
func DetectErrorLoops(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64) {
	if len(turns) < 2 {
		return nil, 0, 0
	}

	engine := pricing.NewEngine(nil)

	var findings []WasteFinding
	var totalWastedToks int64
	var totalWastedUSD float64

	warnThreshold := cfg.WarnConsecutiveToolErrors
	if warnThreshold <= 0 {
		warnThreshold = 3
	}

	type streakItem struct {
		turn  store.Request
		index int
	}

	var currentStreak []streakItem

	processStreak := func(streak []streakItem) {
		if len(streak) < 2 {
			return
		}

		// The repeated failure turns are from the 2nd failure onwards (index 1 to len-1)
		var loopTokens int64
		var loopUSD float64

		for _, item := range streak[1:] {
			turn := item.turn
			toks := turn.InputTokens + turn.OutputTokens
			if turn.TotalTokens > toks {
				toks = turn.TotalTokens
			}
			loopTokens += toks

			if turn.CostUSD != nil && *turn.CostUSD > 0 {
				loopUSD += *turn.CostUSD
			} else if engine != nil && turn.Model != "" {
				if cost, ok := engine.CalculateCost(turn.Model, turn.InputTokens, turn.OutputTokens, turn.CacheReadTokens, turn.CacheWriteTokens); ok && cost != nil {
					loopUSD += *cost
				} else {
					loopUSD += float64(toks) * 3.00 / 1_000_000.0
				}
			} else {
				loopUSD += float64(toks) * 3.00 / 1_000_000.0
			}
		}

		severity := SeverityMedium
		if len(streak) >= 5 {
			severity = SeverityCritical
		} else if len(streak) >= warnThreshold {
			severity = SeverityHigh
		}

		startStep := streak[0].turn.StepIndex
		if startStep == 0 {
			startStep = int64(streak[0].index + 1)
		}
		endStep := streak[len(streak)-1].turn.StepIndex
		if endStep == 0 {
			endStep = int64(streak[len(streak)-1].index + 1)
		}

		findings = append(findings, WasteFinding{
			Category:       WasteErrorLoop,
			Severity:       severity,
			TurnIndex:      startStep,
			WastedTokens:   loopTokens,
			WastedUSD:      loopUSD,
			Explanation:    fmt.Sprintf("Detected %d consecutive tool failure turns (turns %d-%d) spinning context without progress.", len(streak), startStep, endStep),
			Recommendation: "Implement retry circuit breakers and escalate tool errors to the user after repeated failures.",
		})

		totalWastedToks += loopTokens
		totalWastedUSD += loopUSD
	}

	for i, turn := range turns {
		if isFailureTurn(turn) {
			currentStreak = append(currentStreak, streakItem{turn: turn, index: i})
		} else {
			processStreak(currentStreak)
			currentStreak = nil
		}
	}
	processStreak(currentStreak)

	return findings, totalWastedToks, totalWastedUSD
}
