package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
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
