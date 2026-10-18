package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestStore_Queries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	tenDaysAgo := now.Add(-10 * 24 * time.Hour)
	twoMonthsAgo := now.Add(-60 * 24 * time.Hour)

	cost1 := 1.50
	cost2 := 0.75
	cost3 := 2.00
	lat1 := int64(1000)
	lat2 := int64(500)
	lat3 := int64(2000)

	reqs := []store.Request{
		{
			ID: "r1", Tool: "claude_code", SessionID: "s1", Timestamp: now,
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200,
			CostUSD: &cost1, LatencyMS: &lat1, Status: "success",
		},
		{
			ID: "r2", Tool: "aider", SessionID: "s2", Timestamp: tenDaysAgo,
			Model: "gpt-4o", InputTokens: 2000, OutputTokens: 400, TotalTokens: 2400,
			CostUSD: &cost2, LatencyMS: &lat2, Status: "success",
		},
		{
			ID: "r3", Tool: "codex", SessionID: "s3", Timestamp: twoMonthsAgo,
			Model: "o3-mini", InputTokens: 500, OutputTokens: 100, TotalTokens: 600,
			CostUSD: &cost3, LatencyMS: &lat3, Status: "success",
		},
	}

	if _, err := st.InsertRequests(reqs); err != nil {
		t.Fatal(err)
	}

	// 1. Overview tests
	// "today" -> only r1
	ovToday, err := st.GetOverview("today")
	if err != nil {
		t.Fatal(err)
	}
	if ovToday.TotalRequests != 1 || ovToday.TotalTokens != 1200 || ovToday.TotalCost != 1.50 {
		t.Fatalf("unexpected today overview: %+v", ovToday)
	}

	// "week" -> only r1
	ovWeek, err := st.GetOverview("week")
	if err != nil {
		t.Fatal(err)
	}
	if ovWeek.TotalRequests != 1 {
		t.Fatalf("unexpected week overview: %+v", ovWeek)
	}

	// "month" -> r1 and r2
	ovMonth, err := st.GetOverview("month")
	if err != nil {
		t.Fatal(err)
	}
	if ovMonth.TotalRequests != 2 || ovMonth.TotalTokens != 3600 {
		t.Fatalf("unexpected month overview: %+v", ovMonth)
	}

	// "all" -> r1, r2, r3
	ovAll, err := st.GetOverview("all")
	if err != nil {
		t.Fatal(err)
	}
	if ovAll.TotalRequests != 3 || ovAll.TotalTokens != 4200 {
		t.Fatalf("unexpected all overview: %+v", ovAll)
	}

	// 2. Cost Over Time
	cot, err := st.GetCostOverTime("month")
	if err != nil {
		t.Fatal(err)
	}
	if len(cot) != 2 {
		t.Fatalf("expected 2 days/tools in month cost over time, got %d", len(cot))
	}

	// 3. Cost By Model
	cbm, err := st.GetCostByModel("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(cbm) != 3 {
		t.Fatalf("expected 3 models, got %d", len(cbm))
	}
	// Highest cost first: o3-mini (2.00)
	if cbm[0].Model != "o3-mini" || cbm[0].Cost != 2.00 {
		t.Fatalf("expected highest cost o3-mini, got %+v", cbm[0])
	}

	// 4. Tokens By Model
	tbm, err := st.GetTokensByModel("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(tbm) != 3 {
		t.Fatalf("expected 3 models, got %d", len(tbm))
	}
	// Highest tokens first: gpt-4o (2000 in, 400 out)
	if tbm[0].Model != "gpt-4o" || tbm[0].InputTokens != 2000 || tbm[0].OutputTokens != 400 {
		t.Fatalf("expected highest tokens gpt-4o, got %+v", tbm[0])
	}

	// 5. Tool Usage
	tu, err := st.GetToolUsage("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(tu) != 3 {
		t.Fatalf("expected 3 tools, got %d", len(tu))
	}

	// 6. Recent Requests
	recent, err := st.GetRecentRequests(2)
	if err != nil {
		t.Fatal(err)
	}
	if len(recent) != 2 {
		t.Fatalf("expected 2 recent requests, got %d", len(recent))
	}
	if recent[0].ID != "r1" {
		t.Fatalf("expected most recent to be r1, got %s", recent[0].ID)
	}

	// Recent requests default limit
	recentAll, err := st.GetRecentRequests(0)
	if err != nil {
		t.Fatal(err)
	}
	if len(recentAll) != 3 {
		t.Fatalf("expected 3 requests, got %d", len(recentAll))
	}

	// Recent requests with period filtering
	recentToday, err := st.GetRecentRequests(10, "today")
	if err != nil {
		t.Fatal(err)
	}
	if len(recentToday) != 1 || recentToday[0].ID != "r1" {
		t.Fatalf("expected 1 recent request today (r1), got %d", len(recentToday))
	}

	recentMonth, err := st.GetRecentRequests(10, "month")
	if err != nil {
		t.Fatal(err)
	}
	if len(recentMonth) != 2 {
		t.Fatalf("expected 2 recent requests this month (r1, r2), got %d", len(recentMonth))
	}
}

func TestStore_AnalyticsQueries(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_queries.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 1. Verify zero state doesn't error or divide by zero
	overview, err := st.GetOverview("all")
	if err != nil {
		t.Fatalf("unexpected error on empty store: %v", err)
	}
	if overview.TotalSessions != 0 || overview.CacheHitRate != 0.0 || overview.CacheSavingsUSD != 0.0 {
		t.Fatalf("expected 0 stats on empty store, got %+v", overview)
	}

	cacheStats, err := st.GetCacheStats("all")
	if err != nil {
		t.Fatalf("unexpected error on cache stats: %v", err)
	}
	if cacheStats.TotalPrompt != 0 || cacheStats.CacheHitRate != 0.0 || cacheStats.SavingsUSD != 0.0 {
		t.Fatalf("expected 0 cache stats, got %+v", cacheStats)
	}

	sessSummaries, err := st.GetSessionSummaries("all", 10)
	if err != nil {
		t.Fatalf("unexpected error on session summaries: %v", err)
	}
	if len(sessSummaries) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessSummaries))
	}

	toolStatsEmpty, err := st.GetToolCallStats("all")
	if err != nil {
		t.Fatalf("unexpected error on empty tool stats: %v", err)
	}
	if len(toolStatsEmpty) != 0 {
		t.Fatalf("expected 0 tool stats, got %d", len(toolStatsEmpty))
	}

	if _, err := st.GetSessionDetail("non-existent"); err == nil {
		t.Fatal("expected error for non-existent session detail, got nil")
	}

	// 2. Populate test data: 2 sessions, multiple turns, tool calls
	sess1 := "sess-alpha"
	sess2 := "sess-beta"
	now := time.Now().UTC()

	req1 := store.Request{
		ID:                "req-1",
		Tool:              "agy",
		SessionID:         sess1,
		Timestamp:         now.Add(-10 * time.Minute),
		Model:             "gemini-2-pro",
		InputTokens:       200, // Miss
		CacheReadTokens:   800, // Hit
		OutputTokens:      100,
		TotalTokens:       1100,
		StepIndex:         0,
		ToolCallCount:     1,
		ToolErrorCount:    0,
		SubagentCallCount: 0,
		ToolCalls: []store.ToolCall{
			{ID: "t-1", RequestID: "req-1", SessionID: sess1, ToolName: "view_file", Status: "success", CreatedAt: now.Add(-10 * time.Minute)},
		},
	}
	req2 := store.Request{
		ID:                "req-2",
		Tool:              "agy",
		SessionID:         sess1,
		Timestamp:         now.Add(-5 * time.Minute),
		Model:             "gemini-2-pro",
		InputTokens:       100, // Miss
		CacheReadTokens:   900, // Hit
		OutputTokens:      150,
		TotalTokens:       1150,
		StepIndex:         1,
		ToolCallCount:     1,
		ToolErrorCount:    1,
		SubagentCallCount: 0,
		ToolCalls: []store.ToolCall{
			{ID: "t-2", RequestID: "req-2", SessionID: sess1, ToolName: "run_command", Status: "error", CreatedAt: now.Add(-5 * time.Minute)},
		},
	}
	req3 := store.Request{
		ID:                "req-3",
		Tool:              "claude_code",
		SessionID:         sess2,
		Timestamp:         now.Add(-2 * time.Minute),
		Model:             "claude-3-7-sonnet",
		InputTokens:       500, // Miss
		CacheReadTokens:   500, // Hit
		OutputTokens:      200,
		TotalTokens:       1200,
		StepIndex:         0,
		ToolCallCount:     1,
		ToolErrorCount:    0,
		SubagentCallCount: 1,
		ToolCalls: []store.ToolCall{
			{ID: "t-3", RequestID: "req-3", SessionID: sess2, ToolName: "invoke_subagent", Status: "success", CreatedAt: now.Add(-2 * time.Minute)},
		},
	}

	if err := st.InsertRequestsWithTools([]store.Request{req1, req2, req3}); err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// 3. Test GetOverview with data
	overview, err = st.GetOverview("all")
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalRequests != 3 || overview.TotalSessions != 2 {
		t.Errorf("expected 3 requests and 2 sessions, got reqs=%d, sess=%d", overview.TotalRequests, overview.TotalSessions)
	}
	if overview.AvgTurnsPerSession < 1.49 || overview.AvgTurnsPerSession > 1.51 {
		t.Errorf("expected avg turns ~1.5, got %f", overview.AvgTurnsPerSession)
	}
	if overview.CacheHitTokens != 2200 || overview.CacheMissTokens != 800 {
		t.Errorf("expected 2200 hit, 800 miss, got %d hit, %d miss", overview.CacheHitTokens, overview.CacheMissTokens)
	}
	// Total prompt = 800 miss + 2200 hit = 3000 total prompt.
	// Hit rate = 2200 / 3000 = 73.33%
	if overview.CacheHitRate < 73.0 || overview.CacheHitRate > 74.0 {
		t.Errorf("expected cache hit rate ~73.33%%, got %f", overview.CacheHitRate)
	}
	if overview.CacheSavingsUSD <= 0 {
		t.Errorf("expected cache savings > 0, got %f", overview.CacheSavingsUSD)
	}
	if overview.TotalToolCalls != 3 {
		t.Errorf("expected 3 tool calls, got %d", overview.TotalToolCalls)
	}
	if overview.TotalToolErrors != 1 {
		t.Errorf("expected 1 tool error, got %d", overview.TotalToolErrors)
	}
	if overview.TotalSubagents != 1 {
		t.Errorf("expected 1 subagent call, got %d", overview.TotalSubagents)
	}

	// 4. Test GetCacheStats
	cacheStats, err = st.GetCacheStats("all")
	if err != nil {
		t.Fatal(err)
	}
	if cacheStats.CacheHitTokens != 2200 || cacheStats.CacheMissTokens != 800 || cacheStats.OutputTokens != 450 {
		t.Errorf("unexpected cache stats: %+v", cacheStats)
	}
	if cacheStats.TotalPrompt != 3000 {
		t.Errorf("expected 3000 total prompt, got %d", cacheStats.TotalPrompt)
	}
	if cacheStats.CacheHitRate < 73.0 || cacheStats.CacheHitRate > 74.0 {
		t.Errorf("expected cache hit rate ~73.33%%, got %f", cacheStats.CacheHitRate)
	}
	if cacheStats.SavingsUSD <= 0 {
		t.Errorf("expected savings > 0, got %f", cacheStats.SavingsUSD)
	}

	// 5. Test GetSessionSummaries
	summaries, err := st.GetSessionSummaries("all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 session summaries, got %d", len(summaries))
	}
	if summaries[0].SessionID != sess2 { // sess2 had latest timestamp
		t.Errorf("expected latest session %s first, got %s", sess2, summaries[0].SessionID)
	}
	if summaries[1].SessionID == sess1 {
		if summaries[1].TurnCount != 2 {
			t.Errorf("expected 2 turns for sess1, got %d", summaries[1].TurnCount)
		}
		if summaries[1].DurationSeconds < 299 || summaries[1].DurationSeconds > 301 {
			t.Errorf("expected ~300s duration for sess1, got %f", summaries[1].DurationSeconds)
		}
		if summaries[1].ToolCallCount != 2 {
			t.Errorf("expected 2 tool calls for sess1, got %d", summaries[1].ToolCallCount)
		}
		if summaries[1].ToolErrorCount != 1 {
			t.Errorf("expected 1 tool error for sess1, got %d", summaries[1].ToolErrorCount)
		}
		if summaries[1].CacheHitTokens != 1700 || summaries[1].CacheMissTokens != 300 {
			t.Errorf("expected 1700 hit, 300 miss for sess1, got %d hit, %d miss", summaries[1].CacheHitTokens, summaries[1].CacheMissTokens)
		}
		if summaries[1].CacheHitRate < 84.9 || summaries[1].CacheHitRate > 85.1 {
			t.Errorf("expected ~85.0%% hit rate for sess1, got %f", summaries[1].CacheHitRate)
		}
	}

	// 6. Test GetSessionDetail
	detail, err := st.GetSessionDetail(sess1)
	if err != nil {
		t.Fatalf("unexpected error getting session detail: %v", err)
	}
	if detail.Summary.SessionID != sess1 {
		t.Errorf("expected session ID %s, got %s", sess1, detail.Summary.SessionID)
	}
	if detail.Summary.TurnCount != 2 {
		t.Errorf("expected 2 turns in summary, got %d", detail.Summary.TurnCount)
	}
	if len(detail.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(detail.Turns))
	}
	if detail.Turns[0].ID != "req-1" || detail.Turns[1].ID != "req-2" {
		t.Errorf("turns not ordered chronologically: [%s, %s]", detail.Turns[0].ID, detail.Turns[1].ID)
	}
	if len(detail.Turns[0].ToolCalls) != 1 || detail.Turns[0].ToolCalls[0].ToolName != "view_file" {
		t.Errorf("unexpected tool calls for turn 0: %+v", detail.Turns[0].ToolCalls)
	}
	if len(detail.Turns[1].ToolCalls) != 1 || detail.Turns[1].ToolCalls[0].ToolName != "run_command" || detail.Turns[1].ToolCalls[0].Status != "error" {
		t.Errorf("unexpected tool calls for turn 1: %+v", detail.Turns[1].ToolCalls)
	}

	// 7. Test GetToolCallStats
	toolStats, err := st.GetToolCallStats("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(toolStats) != 3 {
		t.Fatalf("expected 3 distinct tools, got %d", len(toolStats))
	}
	toolMap := make(map[string]store.ToolCallStat)
	for _, tc := range toolStats {
		toolMap[tc.ToolName] = tc
	}
	if toolMap["run_command"].TotalCalls != 1 || toolMap["run_command"].ErrorCount != 1 {
		t.Errorf("unexpected stat for run_command: %+v", toolMap["run_command"])
	}
	if toolMap["view_file"].TotalCalls != 1 || toolMap["view_file"].ErrorCount != 0 {
		t.Errorf("unexpected stat for view_file: %+v", toolMap["view_file"])
	}
	if toolMap["invoke_subagent"].TotalCalls != 1 || toolMap["invoke_subagent"].ErrorCount != 0 {
		t.Errorf("unexpected stat for invoke_subagent: %+v", toolMap["invoke_subagent"])
	}

	// 8. Test BUG-01: Seeded pricing table and accurate cache savings
	var pricingCount int
	if err := st.DB().QueryRow("SELECT count(*) FROM pricing").Scan(&pricingCount); err != nil {
		t.Fatalf("failed to query pricing table: %v", err)
	}
	if pricingCount == 0 {
		t.Fatal("expected pricing table to be seeded with default prices, but it is empty")
	}

	// Ingest known models and test cache savings accuracy
	savingsReqs := []store.Request{
		{
			ID: "sav-1", Tool: "claude_code", SessionID: "sav-sess", Timestamp: now,
			Model: "claude-opus-4-6", CacheReadTokens: 1_000_000, TotalTokens: 1_000_000,
		},
		{
			ID: "sav-2", Tool: "claude_code", SessionID: "sav-sess", Timestamp: now,
			Model: "claude-3-5-haiku", CacheReadTokens: 1_000_000, TotalTokens: 1_000_000,
		},
	}
	if _, err := st.InsertRequests(savingsReqs); err != nil {
		t.Fatal(err)
	}
	cacheStats, err = st.GetCacheStats("today")
	if err != nil {
		t.Fatal(err)
	}
	// Opus 4.6 savings: (5.00 - 0.50) = 4.50. Haiku savings: (0.80 - 0.08) = 0.72.
	// Expected total savings: 4.50 + 0.72 = 5.22
	expectedSavings := 4.50 + 0.72
	if cacheStats.SavingsUSD < expectedSavings-0.05 || cacheStats.SavingsUSD > expectedSavings+0.05 {
		t.Errorf("expected cache savings ~$%.2f, got $%.2f", expectedSavings, cacheStats.SavingsUSD)
	}

	// 9. Test BUG-03: Empty and wildcard session ID rejection
	if _, err := st.GetSessionDetail(""); err == nil {
		t.Error("expected error for empty session ID in GetSessionDetail, got nil")
	}
	if _, err := st.GetSessionDetail("   "); err == nil {
		t.Error("expected error for whitespace session ID in GetSessionDetail, got nil")
	}
	if _, err := st.GetSessionDetail("%"); err == nil {
		t.Error("expected error for '%' wildcard session ID, got nil")
	}
	if _, err := st.GetSessionDetail("_"); err == nil {
		t.Error("expected error for '_' wildcard session ID, got nil")
	}

	// 10. Test BUG-06: Indexes and Foreign Keys
	var fkCheck int
	if err := st.DB().QueryRow("PRAGMA foreign_keys").Scan(&fkCheck); err != nil || fkCheck != 1 {
		t.Errorf("expected PRAGMA foreign_keys=1, got %d (err: %v)", fkCheck, err)
	}

	// Verify indexes exist
	for _, idx := range []string{"idx_requests_session", "idx_tool_calls_request"} {
		var idxExists int
		_ = st.DB().QueryRow("SELECT count(*) FROM sqlite_master WHERE type='index' AND name=?", idx).Scan(&idxExists)
		if idxExists == 0 {
			t.Errorf("expected index %s to exist in database", idx)
		}
	}
}

