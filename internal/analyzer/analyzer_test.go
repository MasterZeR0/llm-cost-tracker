package analyzer_test

import (
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func ptrFloat64(f float64) *float64 {
	return &f
}

func ptrInt64(i int64) *int64 {
	return &i
}

func TestAnalyzeSession_CleanSession(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 5-turn steady session with high cache hit and 0 errors
	turns := make([]store.Request, 5)
	for i := 0; i < 5; i++ {
		turns[i] = store.Request{
			ID:              "req-clean",
			SessionID:       "sess-clean-1",
			Tool:            "claude-code",
			Timestamp:       time.Now().Add(time.Duration(i) * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       int64(i + 1),
			InputTokens:     5000,
			CacheReadTokens: 20000,
			OutputTokens:    1000,
			TotalTokens:     26000,
			CostUSD:         ptrFloat64(0.025),
			Status:          "success",
			ToolCallCount:   1,
			ToolErrorCount:  0,
		}
	}

	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-clean-1",
			Tool:        "claude-code",
			TotalTokens: 130000,
			TotalCost:   0.125,
			TurnCount:   5,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)

	if analysis.HealthScore != 100 {
		t.Fatalf("expected HealthScore 100 for clean session, got %d", analysis.HealthScore)
	}
	if analysis.WastedTokens != 0 {
		t.Fatalf("expected 0 wasted tokens, got %d", analysis.WastedTokens)
	}
	if analysis.WastedUSD != 0.0 {
		t.Fatalf("expected 0.0 wasted USD, got %f", analysis.WastedUSD)
	}
	if analysis.WastePercentage != 0.0 {
		t.Fatalf("expected 0.0%% waste, got %f", analysis.WastePercentage)
	}
	if len(analysis.Findings) != 0 {
		t.Fatalf("expected 0 findings, got %d", len(analysis.Findings))
	}
	if len(analysis.Trajectory) != 5 {
		t.Fatalf("expected trajectory length 5, got %d", len(analysis.Trajectory))
	}
	for i, tm := range analysis.Trajectory {
		if tm.IsWaste {
			t.Fatalf("expected turn %d IsWaste to be false", i+1)
		}
	}
}

func TestAnalyzeSession_ContextSnowball(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 12-turn session:
	// turns 1-8: steady small context (10k tokens each, early median ~10k)
	// turns 9-12: explode quadratically up to 140k tokens
	turns := make([]store.Request, 12)
	for i := 0; i < 8; i++ {
		turns[i] = store.Request{
			SessionID:       "sess-snowball-1",
			Tool:            "claude-code",
			Model:           "claude-3-5-sonnet",
			StepIndex:       int64(i + 1),
			InputTokens:     10000,
			OutputTokens:    500,
			CacheReadTokens: 2000,
			TotalTokens:     12500,
			CostUSD:         ptrFloat64(0.038),
			Status:          "success",
		}
	}

	// Late turns (9 to 12): 50k, 90k, 110k, 140k input tokens
	lateTokenCounts := []int64{50000, 90000, 110000, 140000}
	for idx, toks := range lateTokenCounts {
		turnIdx := 8 + idx
		cost := float64(toks) * 3.0 / 1_000_000.0 // ~$0.15 - $0.42
		turns[turnIdx] = store.Request{
			SessionID:       "sess-snowball-1",
			Tool:            "claude-code",
			Model:           "claude-3-5-sonnet",
			StepIndex:       int64(turnIdx + 1),
			InputTokens:     toks,
			OutputTokens:    1000,
			CacheReadTokens: 5000,
			TotalTokens:     toks + 6000,
			CostUSD:         ptrFloat64(cost),
			Status:          "success",
		}
	}

	var totalTokens int64
	var totalCost float64
	for _, tReq := range turns {
		totalTokens += tReq.TotalTokens
		totalCost += *tReq.CostUSD
	}

	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-snowball-1",
			Tool:        "claude-code",
			TotalTokens: totalTokens,
			TotalCost:   totalCost,
			TurnCount:   12,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)

	if len(analysis.Findings) == 0 {
		t.Fatal("expected at least 1 finding for context snowball, got 0")
	}

	foundSnowball := false
	for _, f := range analysis.Findings {
		if f.Category == analyzer.WasteContextSnowball {
			foundSnowball = true
			if f.Severity != analyzer.SeverityCritical {
				t.Errorf("expected Critical severity due to 140k token turn, got %s", f.Severity)
			}
			if f.WastedTokens <= 0 {
				t.Errorf("expected positive wasted tokens, got %d", f.WastedTokens)
			}
			if f.WastedUSD <= 0 {
				t.Errorf("expected positive wasted USD, got %f", f.WastedUSD)
			}
		}
	}

	if !foundSnowball {
		t.Fatal("did not find WasteContextSnowball finding")
	}

	if analysis.HealthScore >= 70 {
		t.Fatalf("expected HealthScore < 70, got %d", analysis.HealthScore)
	}

	if analysis.WastePercentage <= 0 {
		t.Fatalf("expected positive waste percentage, got %f", analysis.WastePercentage)
	}

	// Verify late turns are flagged as waste in trajectory
	wasteCount := 0
	for _, tm := range analysis.Trajectory {
		if tm.IsWaste {
			wasteCount++
		}
	}
	if wasteCount < 3 {
		t.Fatalf("expected at least 3 turns marked as waste in trajectory, got %d", wasteCount)
	}
}

func TestAnalyzeSession_CacheChurn(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 6 turns:
	// Turns 1-5: High cache hit (input: 10,000, cache_read: 40,000 -> 80% cache hit rate)
	// Turn 6: Cache churn! (input: 45,000, cache_read: 5,000 -> 10% hit rate, 50k prompt)
	turns := make([]store.Request, 6)
	for i := 0; i < 5; i++ {
		turns[i] = store.Request{
			SessionID:       "sess-churn-1",
			Tool:            "claude-code",
			Model:           "claude-3-5-sonnet",
			StepIndex:       int64(i + 1),
			InputTokens:     10000,
			CacheReadTokens: 40000,
			OutputTokens:    1000,
			TotalTokens:     51000,
			CostUSD:         ptrFloat64(0.045),
			Status:          "success",
		}
	}

	// Turn 6: Sharp drop
	turns[5] = store.Request{
		SessionID:       "sess-churn-1",
		Tool:            "claude-code",
		Model:           "claude-3-5-sonnet",
		StepIndex:       6,
		InputTokens:     45000,
		CacheReadTokens: 5000,
		OutputTokens:    1000,
		TotalTokens:     51000,
		CostUSD:         ptrFloat64(0.15),
		Status:          "success",
	}

	var totalTokens int64
	var totalCost float64
	for _, tReq := range turns {
		totalTokens += tReq.TotalTokens
		totalCost += *tReq.CostUSD
	}

	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-churn-1",
			Tool:        "claude-code",
			TotalTokens: totalTokens,
			TotalCost:   totalCost,
			TurnCount:   6,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)

	foundChurn := false
	for _, f := range analysis.Findings {
		if f.Category == analyzer.WasteCacheChurn {
			foundChurn = true
			if f.TurnIndex != 6 {
				t.Errorf("expected finding on turn 6, got %d", f.TurnIndex)
			}
			if f.WastedTokens <= 0 {
				t.Errorf("expected positive churned tokens, got %d", f.WastedTokens)
			}
			if f.WastedUSD <= 0 {
				t.Errorf("expected positive churn wasted USD, got %f", f.WastedUSD)
			}
		}
	}

	if !foundChurn {
		t.Fatal("expected WasteCacheChurn finding, none found")
	}

	if analysis.HealthScore >= 100 {
		t.Fatalf("expected HealthScore < 100, got %d", analysis.HealthScore)
	}

	// Turn 6 should be marked as waste in trajectory
	if !analysis.Trajectory[5].IsWaste {
		t.Fatalf("expected turn 6 trajectory to have IsWaste=true")
	}
}

func TestAnalyzeSession_ToolErrorLoop(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 5-turn session:
	// Turn 1: success
	// Turn 2: tool error (1st failure)
	// Turn 3: tool error (2nd failure - retry 1)
	// Turn 4: tool error (3rd failure - retry 2)
	// Turn 5: success
	turns := make([]store.Request, 5)
	for i := 0; i < 5; i++ {
		toolErr := 0
		st := "success"
		if i >= 1 && i <= 3 {
			toolErr = 1
			st = "error"
		}
		turns[i] = store.Request{
			SessionID:      "sess-error-1",
			Tool:           "gemini-cli",
			Model:          "gemini-2.0-flash",
			StepIndex:      int64(i + 1),
			InputTokens:    8000,
			OutputTokens:   400,
			TotalTokens:    8400,
			CostUSD:        ptrFloat64(0.01),
			Status:         st,
			ToolCallCount:  2,
			ToolErrorCount: toolErr,
		}
	}

	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-error-1",
			Tool:        "gemini-cli",
			TotalTokens: 42000,
			TotalCost:   0.05,
			TurnCount:   5,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)

	foundLoop := false
	for _, f := range analysis.Findings {
		if f.Category == analyzer.WasteErrorLoop {
			foundLoop = true
			if f.Severity != analyzer.SeverityHigh {
				t.Errorf("expected SeverityHigh for 3 consecutive errors, got %s", f.Severity)
			}
			// Retries are turns 3 and 4 -> 8400 * 2 = 16800 tokens
			if f.WastedTokens != 16800 {
				t.Errorf("expected 16800 wasted tokens from 2 retry turns, got %d", f.WastedTokens)
			}
			if f.WastedUSD < 0.019 || f.WastedUSD > 0.021 {
				t.Errorf("expected ~$0.02 wasted USD, got %f", f.WastedUSD)
			}
		}
	}

	if !foundLoop {
		t.Fatal("expected WasteErrorLoop finding, none found")
	}

	if analysis.HealthScore >= 100 {
		t.Fatalf("expected HealthScore < 100, got %d", analysis.HealthScore)
	}

	// Turns 3 and 4 should be marked as waste in trajectory
	if !analysis.Trajectory[2].IsWaste {
		t.Errorf("expected turn 3 to be marked IsWaste")
	}
	if !analysis.Trajectory[3].IsWaste {
		t.Errorf("expected turn 4 to be marked IsWaste")
	}
}

func TestAnalyzeSession_MixedSession(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 10 turns with both error loop and cache churn
	turns := make([]store.Request, 10)
	for i := 0; i < 10; i++ {
		toolErr := 0
		st := "success"
		cacheRead := int64(30000)
		input := int64(5000)

		// Error loop on turns 2 and 3
		if i == 1 || i == 2 {
			toolErr = 1
			st = "error"
		}
		// Cache churn on turn 7
		if i == 6 {
			cacheRead = 2000
			input = 35000
		}

		turns[i] = store.Request{
			SessionID:       "sess-mixed-1",
			Tool:            "claude-code",
			Model:           "claude-3-5-sonnet",
			StepIndex:       int64(i + 1),
			InputTokens:     input,
			CacheReadTokens: cacheRead,
			OutputTokens:    1000,
			TotalTokens:     input + cacheRead + 1000,
			CostUSD:         ptrFloat64(0.04),
			Status:          st,
			ToolCallCount:   1,
			ToolErrorCount:  toolErr,
		}
	}

	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-mixed-1",
			Tool:        "claude-code",
			TotalTokens: 350000,
			TotalCost:   0.40,
			TurnCount:   10,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)

	if len(analysis.Findings) < 2 {
		t.Fatalf("expected at least 2 findings (error loop + cache churn), got %d", len(analysis.Findings))
	}

	var hasChurn, hasErrorLoop bool
	for _, f := range analysis.Findings {
		if f.Category == analyzer.WasteCacheChurn {
			hasChurn = true
		}
		if f.Category == analyzer.WasteErrorLoop {
			hasErrorLoop = true
		}
	}

	if !hasChurn || !hasErrorLoop {
		t.Fatalf("expected both cache churn and error loop findings, got churn=%v, errorLoop=%v", hasChurn, hasErrorLoop)
	}

	if analysis.TotalCost != 0.40 {
		t.Errorf("expected TotalCost 0.40, got %f", analysis.TotalCost)
	}
	if analysis.WastedUSD > analysis.TotalCost {
		t.Fatalf("wasted USD (%f) cannot exceed total cost (%f)", analysis.WastedUSD, analysis.TotalCost)
	}
	if analysis.HealthScore >= 90 {
		t.Fatalf("expected HealthScore < 90 for mixed issue session, got %d", analysis.HealthScore)
	}
}

func TestAnalyzeSession_EdgeCases(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// Edge case 1: 0 turns
	analysis0 := analyzer.AnalyzeSession(store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-0"},
		Turns:   nil,
	}, cfg)
	if analysis0.HealthScore != 100 || analysis0.WastedUSD != 0 || analysis0.WastedTokens != 0 {
		t.Fatalf("expected 0-turn session to be 100 health, got %+v", analysis0)
	}

	// Edge case 2: 1 turn
	analysis1 := analyzer.AnalyzeSession(store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-1", TotalCost: 0.05, TurnCount: 1},
		Turns: []store.Request{
			{
				SessionID:   "sess-1",
				StepIndex:   1,
				InputTokens: 130000, // severe token count on single turn
				CostUSD:     ptrFloat64(0.05),
			},
		},
	}, cfg)
	if analysis1.HealthScore != 100 || analysis1.WastedUSD != 0 {
		t.Fatalf("expected 1-turn session to return 100 health, got %+v", analysis1)
	}

	// Edge case 3: Nil costs on turns
	turnsNilCost := []store.Request{
		{StepIndex: 1, InputTokens: 5000, TotalTokens: 5000},
		{StepIndex: 2, InputTokens: 5000, TotalTokens: 5000},
		{StepIndex: 3, InputTokens: 5000, TotalTokens: 5000},
	}
	analysisNil := analyzer.AnalyzeSession(store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-nil-cost"},
		Turns:   turnsNilCost,
	}, cfg)
	if analysisNil.HealthScore != 100 {
		t.Fatalf("expected 100 health on nil cost clean session, got %d", analysisNil.HealthScore)
	}

	// Edge case 4: Disabled config
	disabledCfg := analyzer.DefaultConfig()
	disabledCfg.Enabled = false
	analysisDisabled := analyzer.AnalyzeSession(store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-disabled"},
		Turns: []store.Request{
			{StepIndex: 1, InputTokens: 150000, ToolErrorCount: 1},
			{StepIndex: 2, InputTokens: 150000, ToolErrorCount: 1},
		},
	}, disabledCfg)
	if analysisDisabled.HealthScore != 100 || analysisDisabled.WastedUSD != 0 {
		t.Fatalf("expected 100 health when analyzer is disabled, got %+v", analysisDisabled)
	}
}

func TestAnalyzePeriod_Aggregation(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// Create 5 sessions:
	// Session 1: Clean (0 waste, 100 health)
	// Session 2: Error Loop ($0.06 waste, health ~80)
	// Session 3: Context Snowball ($0.50 waste, health ~40)
	// Session 4: Cache Churn ($0.10 waste, health ~85)
	// Session 5: 1-turn edge case (0 waste, 100 health)

	// Sess 1: Clean
	sess1 := store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-1", TotalCost: 0.10, TurnCount: 3},
		Turns: []store.Request{
			{SessionID: "sess-1", StepIndex: 1, InputTokens: 1000, TotalTokens: 1000, CostUSD: ptrFloat64(0.03)},
			{SessionID: "sess-1", StepIndex: 2, InputTokens: 1000, TotalTokens: 1000, CostUSD: ptrFloat64(0.03)},
			{SessionID: "sess-1", StepIndex: 3, InputTokens: 1000, TotalTokens: 1000, CostUSD: ptrFloat64(0.04)},
		},
	}

	// Sess 2: Error loop
	sess2 := store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-2", TotalCost: 0.12, TurnCount: 4},
		Turns: []store.Request{
			{SessionID: "sess-2", StepIndex: 1, InputTokens: 2000, TotalTokens: 2000, CostUSD: ptrFloat64(0.03)},
			{SessionID: "sess-2", StepIndex: 2, InputTokens: 2000, TotalTokens: 2000, CostUSD: ptrFloat64(0.03), ToolErrorCount: 1},
			{SessionID: "sess-2", StepIndex: 3, InputTokens: 2000, TotalTokens: 2000, CostUSD: ptrFloat64(0.03), ToolErrorCount: 1},
			{SessionID: "sess-2", StepIndex: 4, InputTokens: 2000, TotalTokens: 2000, CostUSD: ptrFloat64(0.03), ToolErrorCount: 1},
		},
	}

	// Sess 3: Context snowball (10 turns, last 3 explode)
	sess3Turns := make([]store.Request, 10)
	for i := 0; i < 7; i++ {
		sess3Turns[i] = store.Request{
			SessionID:   "sess-3",
			StepIndex:   int64(i + 1),
			InputTokens: 5000,
			TotalTokens: 5000,
			CostUSD:     ptrFloat64(0.015),
			Model:       "claude-3-5-sonnet",
		}
	}
	sess3Turns[7] = store.Request{SessionID: "sess-3", StepIndex: 8, InputTokens: 50000, TotalTokens: 50000, CostUSD: ptrFloat64(0.15), Model: "claude-3-5-sonnet"}
	sess3Turns[8] = store.Request{SessionID: "sess-3", StepIndex: 9, InputTokens: 90000, TotalTokens: 90000, CostUSD: ptrFloat64(0.27), Model: "claude-3-5-sonnet"}
	sess3Turns[9] = store.Request{SessionID: "sess-3", StepIndex: 10, InputTokens: 130000, TotalTokens: 130000, CostUSD: ptrFloat64(0.39), Model: "claude-3-5-sonnet"}
	sess3 := store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-3", TotalCost: 0.915, TurnCount: 10},
		Turns:   sess3Turns,
	}

	// Sess 4: Cache Churn
	sess4Turns := make([]store.Request, 4)
	for i := 0; i < 3; i++ {
		sess4Turns[i] = store.Request{
			SessionID:       "sess-4",
			StepIndex:       int64(i + 1),
			InputTokens:     5000,
			CacheReadTokens: 25000,
			TotalTokens:     30000,
			CostUSD:         ptrFloat64(0.02),
			Model:           "claude-3-5-sonnet",
		}
	}
	sess4Turns[3] = store.Request{
		SessionID:       "sess-4",
		StepIndex:       4,
		InputTokens:     30000,
		CacheReadTokens: 2000,
		TotalTokens:     32000,
		CostUSD:         ptrFloat64(0.09),
		Model:           "claude-3-5-sonnet",
	}
	sess4 := store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-4", TotalCost: 0.15, TurnCount: 4},
		Turns:   sess4Turns,
	}

	// Sess 5: 1 turn
	sess5 := store.SessionDetail{
		Summary: store.SessionSummary{SessionID: "sess-5", TotalCost: 0.02, TurnCount: 1},
		Turns: []store.Request{
			{SessionID: "sess-5", StepIndex: 1, InputTokens: 2000, TotalTokens: 2000, CostUSD: ptrFloat64(0.02)},
		},
	}

	sessions := []store.SessionDetail{sess1, sess2, sess3, sess4, sess5}

	summary := analyzer.AnalyzePeriod(sessions, "today", cfg)

	if summary.TotalSessions != 5 {
		t.Fatalf("expected TotalSessions 5, got %d", summary.TotalSessions)
	}
	if summary.WastedSessions != 3 {
		t.Fatalf("expected 3 WastedSessions, got %d", summary.WastedSessions)
	}
	if len(summary.TopWasteSessions) != 3 {
		t.Fatalf("expected 3 TopWasteSessions, got %d", len(summary.TopWasteSessions))
	}

	// Top waste sessions should be sorted by WastedUSD descending:
	// sess-3 should be first (highest waste USD)
	if summary.TopWasteSessions[0].SessionID != "sess-3" {
		t.Fatalf("expected sess-3 to be #1 top waste session, got %s", summary.TopWasteSessions[0].SessionID)
	}

	// Check category totals
	if summary.CategoryTotals[analyzer.WasteContextSnowball] <= 0 {
		t.Errorf("expected positive total for WasteContextSnowball, got %f", summary.CategoryTotals[analyzer.WasteContextSnowball])
	}
	if summary.CategoryTotals[analyzer.WasteErrorLoop] <= 0 {
		t.Errorf("expected positive total for WasteErrorLoop, got %f", summary.CategoryTotals[analyzer.WasteErrorLoop])
	}
	if summary.CategoryTotals[analyzer.WasteCacheChurn] <= 0 {
		t.Errorf("expected positive total for WasteCacheChurn, got %f", summary.CategoryTotals[analyzer.WasteCacheChurn])
	}

	if summary.TotalWastedUSD <= 0 {
		t.Fatalf("expected positive TotalWastedUSD, got %f", summary.TotalWastedUSD)
	}
	if summary.TotalWastedToks <= 0 {
		t.Fatalf("expected positive TotalWastedToks, got %d", summary.TotalWastedToks)
	}
	if summary.AvgHealthScore <= 0 || summary.AvgHealthScore > 100 {
		t.Fatalf("expected AvgHealthScore between 0 and 100, got %f", summary.AvgHealthScore)
	}
}

func TestAnalyzeSession_UnpricedAndFreeModels(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	t.Run("UnpricedModel_NilCost", func(t *testing.T) {
		// Session with unmetered/unpriced model (CostUSD is nil)
		// 6 turns with tool error loop and severe context growth
		turns := make([]store.Request, 6)
		for i := 0; i < 6; i++ {
			toolErr := 0
			st := "success"
			if i >= 1 && i <= 3 {
				toolErr = 1
				st = "error"
			}
			input := int64(8000)
			if i == 5 {
				input = 130000 // severe token turn
			}

			turns[i] = store.Request{
				SessionID:      "sess-unpriced-1",
				Tool:           "ollama-cli",
				Model:          "unpriced-local-model-xyz",
				StepIndex:      int64(i + 1),
				InputTokens:    input,
				OutputTokens:   500,
				TotalTokens:    input + 500,
				CostUSD:        nil,
				Status:         st,
				ToolCallCount:  1,
				ToolErrorCount: toolErr,
			}
		}

		detail := store.SessionDetail{
			Summary: store.SessionSummary{
				SessionID:   "sess-unpriced-1",
				Tool:        "ollama-cli",
				TotalTokens: 180000,
				TotalCost:   0.0,
				TurnCount:   6,
			},
			Turns: turns,
		}

		analysis := analyzer.AnalyzeSession(detail, cfg)

		// Verification: TotalCost == 0.0 and WastedUSD == 0.0 (never > TotalCost)
		if analysis.TotalCost != 0.0 {
			t.Fatalf("expected TotalCost 0.0, got %f", analysis.TotalCost)
		}
		if analysis.WastedUSD != 0.0 {
			t.Fatalf("expected WastedUSD 0.0 for unpriced session, got %f (must never exceed TotalCost)", analysis.WastedUSD)
		}
		if analysis.WastedUSD > analysis.TotalCost {
			t.Fatalf("WastedUSD (%f) must never exceed TotalCost (%f)", analysis.WastedUSD, analysis.TotalCost)
		}

		// Token waste should still be detected
		if analysis.WastedTokens <= 0 {
			t.Fatalf("expected positive WastedTokens, got %d", analysis.WastedTokens)
		}
		if len(analysis.Findings) == 0 {
			t.Fatalf("expected findings to be identified for tool errors and large context")
		}
		for _, f := range analysis.Findings {
			if f.WastedUSD != 0.0 {
				t.Fatalf("expected finding WastedUSD 0.0, got %f for category %s", f.WastedUSD, f.Category)
			}
		}

		// WastePercentage should be calculated from tokens when TotalCost == 0
		if analysis.WastePercentage <= 0.0 {
			t.Fatalf("expected positive WastePercentage based on token waste, got %f", analysis.WastePercentage)
		}
		if analysis.HealthScore >= 100 {
			t.Fatalf("expected HealthScore < 100 due to errors and context bloat, got %d", analysis.HealthScore)
		}
	})

	t.Run("FreeModel_ExplicitZeroCost", func(t *testing.T) {
		// Session where turns have explicit CostUSD = 0.0
		turns := make([]store.Request, 5)
		for i := 0; i < 5; i++ {
			toolErr := 0
			st := "success"
			if i >= 1 && i <= 3 {
				toolErr = 1
				st = "error"
			}
			turns[i] = store.Request{
				SessionID:      "sess-free-zero",
				Tool:           "gemini-free-cli",
				Model:          "gemini-2.0-flash",
				StepIndex:      int64(i + 1),
				InputTokens:    5000,
				OutputTokens:   200,
				TotalTokens:    5200,
				CostUSD:        ptrFloat64(0.0), // explicit 0.0 USD
				Status:         st,
				ToolCallCount:  1,
				ToolErrorCount: toolErr,
			}
		}

		detail := store.SessionDetail{
			Summary: store.SessionSummary{
				SessionID:   "sess-free-zero",
				Tool:        "gemini-free-cli",
				TotalTokens: 26000,
				TotalCost:   0.0,
				TurnCount:   5,
			},
			Turns: turns,
		}

		analysis := analyzer.AnalyzeSession(detail, cfg)

		if analysis.TotalCost != 0.0 {
			t.Fatalf("expected TotalCost 0.0, got %f", analysis.TotalCost)
		}
		if analysis.WastedUSD != 0.0 {
			t.Fatalf("expected WastedUSD 0.0 for free session, got %f", analysis.WastedUSD)
		}
		if analysis.WastedUSD > analysis.TotalCost {
			t.Fatalf("WastedUSD (%f) must never exceed TotalCost (%f)", analysis.WastedUSD, analysis.TotalCost)
		}
		for _, f := range analysis.Findings {
			if f.WastedUSD != 0.0 {
				t.Fatalf("expected finding WastedUSD 0.0, got %f for category %s", f.WastedUSD, f.Category)
			}
		}
	})
}

func TestAnalyzeSession_ZeroDiscountModel_CacheChurn(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	// 6 turns with cache churn on a model with zero prompt cache discount (InputPerMTok == CacheReadPerMTok)
	turns := make([]store.Request, 6)
	for i := 0; i < 5; i++ {
		turns[i] = store.Request{
			SessionID:       "sess-zero-discount-1",
			Tool:            "claude-code",
			Model:           "zero-discount-model",
			StepIndex:       int64(i + 1),
			InputTokens:     10000,
			CacheReadTokens: 40000,
			OutputTokens:    1000,
			TotalTokens:     51000,
			CostUSD:         ptrFloat64(0.153),
			Status:          "success",
		}
	}

	// Turn 6: Cache churn drops hit rate from 80% to 10%
	turns[5] = store.Request{
		SessionID:       "sess-zero-discount-1",
		Tool:            "claude-code",
		Model:           "zero-discount-model",
		StepIndex:       6,
		InputTokens:     45000,
		CacheReadTokens: 5000,
		OutputTokens:    1000,
		TotalTokens:     51000,
		CostUSD:         ptrFloat64(0.153),
		Status:          "success",
	}

	// Verify direct DetectCacheChurn output
	churnFindings, churnToks, churnCost := analyzer.DetectCacheChurn(turns, cfg)
	if len(churnFindings) == 0 {
		t.Fatal("expected at least 1 cache churn finding")
	}
	if churnToks <= 0 {
		t.Fatalf("expected positive churned tokens, got %d", churnToks)
	}
	// Churn cost must be exactly 0.0 USD because cache read price equals input price (delta <= 0)
	// It must NOT fall through to the $2.70/M fallback rate!
	if churnCost != 0.0 {
		t.Fatalf("expected 0.0 churn USD waste for zero-discount model, got %f (fell through to fallback rate!)", churnCost)
	}
	for _, f := range churnFindings {
		if f.WastedUSD != 0.0 {
			t.Fatalf("expected finding WastedUSD 0.0, got %f", f.WastedUSD)
		}
	}

	// Also verify full AnalyzeSession integration
	detail := store.SessionDetail{
		Summary: store.SessionSummary{
			SessionID:   "sess-zero-discount-1",
			Tool:        "claude-code",
			TotalTokens: 306000,
			TotalCost:   0.918,
			TurnCount:   6,
		},
		Turns: turns,
	}

	analysis := analyzer.AnalyzeSession(detail, cfg)
	foundChurn := false
	for _, f := range analysis.Findings {
		if f.Category == analyzer.WasteCacheChurn {
			foundChurn = true
			if f.WastedUSD != 0.0 {
				t.Fatalf("expected churn finding WastedUSD 0.0, got %f", f.WastedUSD)
			}
		}
	}
	if !foundChurn {
		t.Fatal("expected WasteCacheChurn finding in AnalyzeSession output")
	}
	if analysis.WastedUSD != 0.0 {
		t.Fatalf("expected 0.0 WastedUSD from zero-discount cache churn, got %f", analysis.WastedUSD)
	}
}
