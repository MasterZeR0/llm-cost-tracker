package dashboard_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/dashboard"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestDashboardAPI_Overview(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost := 0.50
	latency := int64(800)
	_, _ = st.InsertRequests([]store.Request{
		{
			ID: "r1", Tool: "claude_code", Timestamp: time.Now().UTC(),
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200,
			CostUSD: &cost, LatencyMS: &latency, Status: "success",
		},
	})

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	req := httptest.NewRequest("GET", "/api/overview?period=all", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var stats store.OverviewStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.TotalRequests)
	}
	if stats.TotalTokens != 1200 {
		t.Fatalf("expected 1200 tokens, got %d", stats.TotalTokens)
	}
}

func TestDashboardAPI_AllEndpoints(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost1 := 0.50
	latency1 := int64(800)
	cost2 := 0.25
	latency2 := int64(400)
	now := time.Now().UTC()

	_, err = st.InsertRequests([]store.Request{
		{
			ID: "r1", Tool: "claude_code", Timestamp: now,
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200,
			CostUSD: &cost1, LatencyMS: &latency1, Status: "success",
		},
		{
			ID: "r2", Tool: "aider", Timestamp: now,
			Model: "gpt-4o", InputTokens: 500, OutputTokens: 100, TotalTokens: 600,
			CostUSD: &cost2, LatencyMS: &latency2, Status: "success",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	// 1. Cost over time
	req := httptest.NewRequest("GET", "/api/cost-over-time?period=all", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cost-over-time: expected 200, got %d", w.Code)
	}
	var timeSeries []store.TimeSeriesPoint
	if err := json.Unmarshal(w.Body.Bytes(), &timeSeries); err != nil {
		t.Fatal(err)
	}
	if len(timeSeries) == 0 {
		t.Fatalf("expected time series points, got 0")
	}

	// 2. Cost by model
	req = httptest.NewRequest("GET", "/api/cost-by-model?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("cost-by-model: expected 200, got %d", w.Code)
	}
	var modelCosts []store.ModelCostPoint
	if err := json.Unmarshal(w.Body.Bytes(), &modelCosts); err != nil {
		t.Fatal(err)
	}
	if len(modelCosts) != 2 {
		t.Fatalf("expected 2 model cost points, got %d", len(modelCosts))
	}

	// 3. Tokens by model
	req = httptest.NewRequest("GET", "/api/tokens-by-model?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tokens-by-model: expected 200, got %d", w.Code)
	}
	var modelTokens []store.ModelTokenPoint
	if err := json.Unmarshal(w.Body.Bytes(), &modelTokens); err != nil {
		t.Fatal(err)
	}
	if len(modelTokens) != 2 {
		t.Fatalf("expected 2 model token points, got %d", len(modelTokens))
	}

	// 4. Tool usage
	req = httptest.NewRequest("GET", "/api/tool-usage?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("tool-usage: expected 200, got %d", w.Code)
	}
	var toolUsage []store.ToolUsagePoint
	if err := json.Unmarshal(w.Body.Bytes(), &toolUsage); err != nil {
		t.Fatal(err)
	}
	if len(toolUsage) != 2 {
		t.Fatalf("expected 2 tool usage points, got %d", len(toolUsage))
	}

	// 5. Recent requests
	req = httptest.NewRequest("GET", "/api/requests", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("requests: expected 200, got %d", w.Code)
	}
	var reqs []store.Request
	if err := json.Unmarshal(w.Body.Bytes(), &reqs); err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}

	// 6. Static files - index.html
	req = httptest.NewRequest("GET", "/", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("static /: expected 200, got %d", w.Code)
	}
	if !strings.Contains(w.Body.String(), "LLM Cost Tracker") {
		t.Fatalf("expected index.html content to contain 'LLM Cost Tracker'")
	}

	// 7. Recent requests with ?period=
	req = httptest.NewRequest("GET", "/api/requests?period=today", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("requests?period=today: expected 200, got %d", w.Code)
	}
	var todayReqs []store.Request
	if err := json.Unmarshal(w.Body.Bytes(), &todayReqs); err != nil {
		t.Fatal(err)
	}
	if len(todayReqs) != 2 {
		t.Fatalf("expected 2 requests for today, got %d", len(todayReqs))
	}
}
