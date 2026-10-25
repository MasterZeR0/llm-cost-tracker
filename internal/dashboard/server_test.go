package dashboard_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/dashboard"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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

	// 6. Latency over time
	req = httptest.NewRequest("GET", "/api/latency-over-time?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("latency-over-time: expected 200, got %d", w.Code)
	}
	var latencyPoints []store.LatencyPoint
	if err := json.Unmarshal(w.Body.Bytes(), &latencyPoints); err != nil {
		t.Fatal(err)
	}
	if len(latencyPoints) != 1 {
		t.Fatalf("expected 1 latency point, got %d", len(latencyPoints))
	}
	if latencyPoints[0].AvgLatencyMS != 600.0 || latencyPoints[0].RequestCount != 2 {
		t.Fatalf("unexpected latency point: %+v", latencyPoints[0])
	}

	// 7. Model usage
	req = httptest.NewRequest("GET", "/api/model-usage?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("model-usage: expected 200, got %d", w.Code)
	}
	var modelUsage []store.ModelUsagePoint
	if err := json.Unmarshal(w.Body.Bytes(), &modelUsage); err != nil {
		t.Fatal(err)
	}
	if len(modelUsage) != 2 {
		t.Fatalf("expected 2 model usage points, got %d", len(modelUsage))
	}
	for _, mu := range modelUsage {
		if mu.RequestCount != 1 || mu.Percentage != 50.0 {
			t.Fatalf("unexpected model usage point: %+v", mu)
		}
	}

	// 8. Static files - index.html
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

func TestDashboardAPI_AnalyticsAndSessions(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost1 := 0.10
	cost2 := 0.20
	latency1 := int64(500)
	latency2 := int64(700)
	now := time.Now().UTC()

	reqs := []store.Request{
		{
			ID: "r1", Tool: "claude_code", SessionID: "sess-1", Timestamp: now.Add(-time.Minute),
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 200, CacheReadTokens: 500,
			TotalTokens: 1700, CostUSD: &cost1, LatencyMS: &latency1, Status: "success",
			StepIndex: 1, ToolCallCount: 1, ToolErrorCount: 0,
			ToolCalls: []store.ToolCall{
				{
					ID: "tc-1", RequestID: "r1", SessionID: "sess-1",
					ToolName: "ReadFile", Status: "success", CreatedAt: now.Add(-time.Minute),
				},
			},
		},
		{
			ID: "r2", Tool: "claude_code", SessionID: "sess-1", Timestamp: now,
			Model: "claude-3-7-sonnet", InputTokens: 1500, OutputTokens: 300, CacheReadTokens: 1000,
			TotalTokens: 2800, CostUSD: &cost2, LatencyMS: &latency2, Status: "success",
			StepIndex: 2, ToolCallCount: 1, ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID: "tc-2", RequestID: "r2", SessionID: "sess-1",
					ToolName: "RunBash", Status: "error", CreatedAt: now,
				},
			},
		},
	}

	_, err = st.InsertRequests(reqs)
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	// 1. GET /api/analytics/cache
	req := httptest.NewRequest("GET", "/api/analytics/cache?period=all", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("analytics/cache: expected 200, got %d", w.Code)
	}
	var cacheStats store.CacheStats
	if err := json.Unmarshal(w.Body.Bytes(), &cacheStats); err != nil {
		t.Fatal(err)
	}
	if cacheStats.CacheHitTokens != 1500 {
		t.Fatalf("expected 1500 cache hit tokens, got %d", cacheStats.CacheHitTokens)
	}
	if cacheStats.CacheMissTokens != 2500 {
		t.Fatalf("expected 2500 cache miss tokens, got %d", cacheStats.CacheMissTokens)
	}
	if cacheStats.OutputTokens != 500 {
		t.Fatalf("expected 500 output tokens, got %d", cacheStats.OutputTokens)
	}
	if cacheStats.CacheHitRate <= 0 {
		t.Fatalf("expected cache hit rate > 0, got %f", cacheStats.CacheHitRate)
	}

	// 2. GET /api/analytics/tools
	req = httptest.NewRequest("GET", "/api/analytics/tools?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("analytics/tools: expected 200, got %d", w.Code)
	}
	var toolStats []store.ToolCallStat
	if err := json.Unmarshal(w.Body.Bytes(), &toolStats); err != nil {
		t.Fatal(err)
	}
	if len(toolStats) != 2 {
		t.Fatalf("expected 2 tool stats, got %d", len(toolStats))
	}

	// 3. GET /api/sessions
	req = httptest.NewRequest("GET", "/api/sessions?period=all&limit=10", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sessions: expected 200, got %d", w.Code)
	}
	var sessions []store.SessionSummary
	if err := json.Unmarshal(w.Body.Bytes(), &sessions); err != nil {
		t.Fatal(err)
	}
	if len(sessions) != 1 {
		t.Fatalf("expected 1 session, got %d", len(sessions))
	}
	if sessions[0].SessionID != "sess-1" {
		t.Fatalf("expected sess-1, got %s", sessions[0].SessionID)
	}
	if sessions[0].TurnCount != 2 {
		t.Fatalf("expected 2 turns, got %d", sessions[0].TurnCount)
	}

	// 4. GET /api/sessions/detail?id=...
	req = httptest.NewRequest("GET", "/api/sessions/detail?id=sess-1", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sessions/detail query: expected 200, got %d", w.Code)
	}
	var detail store.SessionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &detail); err != nil {
		t.Fatal(err)
	}
	if detail.Summary.SessionID != "sess-1" {
		t.Fatalf("expected session sess-1, got %s", detail.Summary.SessionID)
	}
	if len(detail.Turns) != 2 {
		t.Fatalf("expected 2 turns, got %d", len(detail.Turns))
	}

	// 5. GET /api/sessions/{id}
	req = httptest.NewRequest("GET", "/api/sessions/sess-1", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusOK {
		t.Fatalf("sessions/{id} path: expected 200, got %d", w.Code)
	}
	var pathDetail store.SessionDetail
	if err := json.Unmarshal(w.Body.Bytes(), &pathDetail); err != nil {
		t.Fatal(err)
	}
	if pathDetail.Summary.SessionID != "sess-1" {
		t.Fatalf("expected session sess-1, got %s", pathDetail.Summary.SessionID)
	}

	// 6. Missing session ID returns 400
	req = httptest.NewRequest("GET", "/api/sessions/detail", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing id, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type for 400, got %q", ct)
	}
	var err400 map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &err400); err != nil {
		t.Fatalf("expected JSON body for 400, got: %v", err)
	}
	if err400["error"] != "missing session id" {
		t.Fatalf("expected error 'missing session id', got %q", err400["error"])
	}

	// 7. Non-existent session returns 404 JSON (BUG-02)
	req = httptest.NewRequest("GET", "/api/sessions/detail?id=does-not-exist", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent id, got %d", w.Code)
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON response for 404, got error: %v", err)
	}
	if errResp["error"] != "session not found" {
		t.Fatalf("expected 'session not found', got %q", errResp["error"])
	}

	// 8. Non-existent session via path returns 404 JSON (BUG-02)
	req = httptest.NewRequest("GET", "/api/sessions/does-not-exist", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for nonexistent path id, got %d", w.Code)
	}

	// 9. Wildcard % does not match all sessions (BUG-03)
	req = httptest.NewRequest("GET", "/api/sessions/detail?id=%25", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)
	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for wildcard '%%' session id, got %d", w.Code)
	}
}

func TestDashboardAPI_Waste(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	cost1 := 0.10
	cost2 := 0.20
	latency := int64(600)

	_, err = st.InsertRequests([]store.Request{
		{
			ID: "r1", Tool: "claude_code", SessionID: "sess-w1", Timestamp: now,
			Model: "claude-3-7-sonnet", InputTokens: 5000, OutputTokens: 1000,
			CacheReadTokens: 1000, TotalTokens: 6000, CostUSD: &cost1, LatencyMS: &latency,
			Status: "success", StepIndex: 1,
		},
		{
			ID: "r2", Tool: "claude_code", SessionID: "sess-w1", Timestamp: now.Add(time.Second),
			Model: "claude-3-7-sonnet", InputTokens: 95000, OutputTokens: 2000,
			CacheReadTokens: 500, TotalTokens: 97000, CostUSD: &cost2, LatencyMS: &latency,
			Status: "success", StepIndex: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	// 1. Default period (today)
	req := httptest.NewRequest("GET", "/api/waste", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/waste, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}

	var summary analyzer.PeriodWasteSummary
	if err := json.Unmarshal(w.Body.Bytes(), &summary); err != nil {
		t.Fatalf("failed to unmarshal PeriodWasteSummary: %v", err)
	}
	if summary.Period != "today" {
		t.Fatalf("expected period 'today', got %q", summary.Period)
	}
	if summary.CategoryTotals == nil {
		t.Fatal("expected non-nil CategoryTotals")
	}

	// 2. Explicit period=all
	req = httptest.NewRequest("GET", "/api/waste?period=all", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from /api/waste?period=all, got %d", w.Code)
	}
	var allSummary analyzer.PeriodWasteSummary
	if err := json.Unmarshal(w.Body.Bytes(), &allSummary); err != nil {
		t.Fatalf("failed to unmarshal PeriodWasteSummary for period=all: %v", err)
	}
	if allSummary.Period != "all" {
		t.Fatalf("expected period 'all', got %q", allSummary.Period)
	}
	if allSummary.TotalSessions != 1 {
		t.Fatalf("expected 1 session, got %d", allSummary.TotalSessions)
	}
}

func TestDashboardAPI_SessionWaste(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	cost1 := 0.05
	cost2 := 0.25
	latency := int64(500)

	_, err = st.InsertRequests([]store.Request{
		{
			ID: "r1", Tool: "cursor", SessionID: "sess-inspect-1", Timestamp: now,
			Model: "claude-3-5-sonnet", InputTokens: 2000, OutputTokens: 300,
			TotalTokens: 2300, CostUSD: &cost1, LatencyMS: &latency,
			Status: "success", StepIndex: 1,
		},
		{
			ID: "r2", Tool: "cursor", SessionID: "sess-inspect-1", Timestamp: now.Add(time.Second),
			Model: "claude-3-5-sonnet", InputTokens: 85000, OutputTokens: 500,
			TotalTokens: 85500, CostUSD: &cost2, LatencyMS: &latency,
			Status: "success", StepIndex: 2,
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	// 1. GET /api/sessions/{id}/waste (path param)
	req := httptest.NewRequest("GET", "/api/sessions/sess-inspect-1/waste", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from path /api/sessions/sess-inspect-1/waste, got %d", w.Code)
	}
	ct := w.Header().Get("Content-Type")
	if !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type, got %q", ct)
	}

	var analysis analyzer.SessionWasteAnalysis
	if err := json.Unmarshal(w.Body.Bytes(), &analysis); err != nil {
		t.Fatalf("failed to unmarshal SessionWasteAnalysis: %v", err)
	}
	if analysis.SessionID != "sess-inspect-1" {
		t.Fatalf("expected SessionID sess-inspect-1, got %q", analysis.SessionID)
	}
	if analysis.HealthScore <= 0 || analysis.HealthScore > 100 {
		t.Fatalf("invalid HealthScore %d", analysis.HealthScore)
	}
	if len(analysis.Trajectory) != 2 {
		t.Fatalf("expected 2 trajectory points, got %d", len(analysis.Trajectory))
	}

	// 2. GET /api/sessions/waste?id=sess-inspect-1 (query param)
	req = httptest.NewRequest("GET", "/api/sessions/waste?id=sess-inspect-1", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected 200 from query /api/sessions/waste?id=..., got %d", w.Code)
	}

	// 3. 404 for unknown session via path
	req = httptest.NewRequest("GET", "/api/sessions/unknown-session/waste", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session, got %d", w.Code)
	}
	var errResp map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errResp); err != nil {
		t.Fatalf("expected JSON 404 response: %v", err)
	}
	if errResp["error"] != "session not found" {
		t.Fatalf("expected 'session not found' error, got %q", errResp["error"])
	}

	// 4. 404 for unknown session via query param
	req = httptest.NewRequest("GET", "/api/sessions/waste?id=unknown-session", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusNotFound {
		t.Fatalf("expected 404 for unknown session query, got %d", w.Code)
	}

	// 5. 400 for missing session id
	req = httptest.NewRequest("GET", "/api/sessions/waste", nil)
	w = httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusBadRequest {
		t.Fatalf("expected 400 for missing session id, got %d", w.Code)
	}
	if ct := w.Header().Get("Content-Type"); !strings.Contains(ct, "application/json") {
		t.Fatalf("expected application/json Content-Type for waste 400, got %q", ct)
	}
	var errWaste400 map[string]string
	if err := json.Unmarshal(w.Body.Bytes(), &errWaste400); err != nil {
		t.Fatalf("expected JSON body for waste 400, got: %v", err)
	}
	if errWaste400["error"] != "missing session id" {
		t.Fatalf("expected error 'missing session id', got %q", errWaste400["error"])
	}
}
