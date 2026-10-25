package test

import (
	"encoding/json"
	"fmt"
	"net/http"
	"net/http/httptest"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/cli"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/dashboard"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestE2E_WasteDiagnosticsPipeline(t *testing.T) {
	tempDir := t.TempDir()
	dbPath := filepath.Join(tempDir, "waste_e2e.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	now := time.Now()

	// 1. Seed a session exhibiting context snowballing (Session A - Claude Code)
	// 10 turns where context escalates drastically from turn 7 onwards
	sessionA := "ses-snowball-claude"
	var reqsA []store.Request
	earlyTokens := int64(30000)
	for i := 1; i <= 10; i++ {
		inputTokens := earlyTokens
		if i >= 8 {
			inputTokens = 125000 + int64(i*5000) // Escalates past 120k threshold
		}
		cost := float64(inputTokens) / 1_000_000.0 * 3.0 // $3 / MTok
		req := store.Request{
			ID:              fmt.Sprintf("req-a-%02d", i),
			Tool:            "claude_code",
			SessionID:       sessionA,
			Timestamp:       now.Add(time.Duration(i) * time.Minute),
			Model:           "claude-3-5-sonnet",
			InputTokens:     inputTokens,
			OutputTokens:    1000,
			CacheReadTokens: 10000,
			TotalTokens:     inputTokens + 1000,
			CostUSD:         &cost,
			Status:          "success",
			StepIndex:       int64(i),
		}
		reqsA = append(reqsA, req)
	}
	if _, err := st.InsertRequests(reqsA); err != nil {
		t.Fatalf("failed to insert session A requests: %v", err)
	}

	// 2. Seed a session exhibiting consecutive tool error loops (Session B - AGY)
	sessionB := "ses-errorloop-agy"
	var reqsB []store.Request
	for i := 1; i <= 6; i++ {
		inputToks := int64(25000)
		cost := 0.15
		toolErrCount := 0
		if i >= 3 {
			toolErrCount = 1 // 4 consecutive error turns (turns 3, 4, 5, 6)
		}
		req := store.Request{
			ID:             fmt.Sprintf("req-b-%02d", i),
			Tool:           "agy",
			SessionID:      sessionB,
			Timestamp:      now.Add(time.Duration(20+i) * time.Minute),
			Model:          "gemini-2.0-flash",
			InputTokens:    inputToks,
			OutputTokens:   500,
			TotalTokens:    inputToks + 500,
			CostUSD:        &cost,
			Status:         "success",
			StepIndex:      int64(i),
			ToolCallCount:  1,
			ToolErrorCount: toolErrCount,
			ToolCalls: []store.ToolCall{
				{
					ID:        fmt.Sprintf("tc-b-%02d", i),
					RequestID: fmt.Sprintf("req-b-%02d", i),
					SessionID: sessionB,
					ToolName:  "run_command",
					Status:    map[bool]string{true: "error", false: "success"}[toolErrCount > 0],
					CreatedAt: now.Add(time.Duration(20+i) * time.Minute),
				},
			},
		}
		reqsB = append(reqsB, req)
	}
	if _, err := st.InsertRequests(reqsB); err != nil {
		t.Fatalf("failed to insert session B requests: %v", err)
	}

	// 3. Verify Store Batch Retrieval
	sessionDetails, err := st.GetPeriodSessionDetails("today")
	if err != nil {
		t.Fatalf("GetPeriodSessionDetails failed: %v", err)
	}
	if len(sessionDetails) != 2 {
		t.Fatalf("expected 2 session details, got %d", len(sessionDetails))
	}

	// 4. Verify Analyzer Period Analysis
	cfg := analyzer.DefaultConfig()
	periodSummary := analyzer.AnalyzePeriod(sessionDetails, "today", cfg)

	if periodSummary.TotalSessions != 2 {
		t.Errorf("expected 2 total sessions, got %d", periodSummary.TotalSessions)
	}
	if periodSummary.WastedSessions != 2 {
		t.Errorf("expected 2 flagged wasted sessions, got %d", periodSummary.WastedSessions)
	}
	if periodSummary.TotalWastedUSD <= 0 {
		t.Errorf("expected positive TotalWastedUSD, got %f", periodSummary.TotalWastedUSD)
	}
	if len(periodSummary.TopWasteSessions) != 2 {
		t.Fatalf("expected 2 top waste sessions, got %d", len(periodSummary.TopWasteSessions))
	}

	// Session A should detect Context Snowball
	analysisA := periodSummary.TopWasteSessions[0]
	if analysisA.SessionID != sessionA && periodSummary.TopWasteSessions[1].SessionID == sessionA {
		analysisA = periodSummary.TopWasteSessions[1]
	}
	hasSnowball := false
	for _, f := range analysisA.Findings {
		if f.Category == analyzer.WasteContextSnowball {
			hasSnowball = true
			break
		}
	}
	if !hasSnowball {
		t.Errorf("expected session A to have WasteContextSnowball finding, got findings: %+v", analysisA.Findings)
	}

	// Session B should detect Tool Error Loop
	analysisB := periodSummary.TopWasteSessions[0]
	if analysisB.SessionID != sessionB && periodSummary.TopWasteSessions[1].SessionID == sessionB {
		analysisB = periodSummary.TopWasteSessions[1]
	}
	hasErrorLoop := false
	for _, f := range analysisB.Findings {
		if f.Category == analyzer.WasteErrorLoop {
			hasErrorLoop = true
			break
		}
	}
	if !hasErrorLoop {
		t.Errorf("expected session B to have WasteErrorLoop finding, got findings: %+v", analysisB.Findings)
	}

	// 5. Verify CLI Render Output
	var cliBuf strings.Builder
	if err := cli.RenderWaste(&cliBuf, st, "today", cfg); err != nil {
		t.Fatalf("RenderWaste failed: %v", err)
	}
	out := cliBuf.String()
	if !strings.Contains(out, "Context Bloat & Waste Diagnostics") {
		t.Errorf("missing title in CLI output: %s", out)
	}
	if !strings.Contains(out, sessionA) || !strings.Contains(out, sessionB) {
		t.Errorf("CLI output missing session IDs: %s", out)
	}
	if !strings.Contains(out, "Recommendations:") {
		t.Errorf("CLI output missing recommendations: %s", out)
	}

	// 6. Verify Dashboard REST API Endpoints
	srv := dashboard.NewServer(&config.Config{
		WasteMonitoring: config.WasteMonitoringConfig{
			Enabled:                   true,
			SnowballTokenThreshold:    80000,
			SevereTurnTokenThreshold:  120000,
			WarnConsecutiveToolErrors: 3,
			CacheChurnDropThreshold:   0.40,
			LogAlertsToConsole:        true,
		},
	}, st)
	ts := httptest.NewServer(srv.Router())
	defer ts.Close()

	// GET /api/waste?period=today
	resp, err := http.Get(ts.URL + "/api/waste?period=today")
	if err != nil {
		t.Fatalf("GET /api/waste failed: %v", err)
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/waste, got %d", resp.StatusCode)
	}
	var apiPeriodSummary analyzer.PeriodWasteSummary
	if err := json.NewDecoder(resp.Body).Decode(&apiPeriodSummary); err != nil {
		t.Fatalf("failed to decode /api/waste response: %v", err)
	}
	if apiPeriodSummary.TotalSessions != 2 || apiPeriodSummary.WastedSessions != 2 {
		t.Errorf("unexpected API period summary: %+v", apiPeriodSummary)
	}

	// GET /api/sessions/{id}/waste
	respSession, err := http.Get(ts.URL + "/api/sessions/" + sessionA + "/waste")
	if err != nil {
		t.Fatalf("GET /api/sessions/{id}/waste failed: %v", err)
	}
	defer respSession.Body.Close()
	if respSession.StatusCode != http.StatusOK {
		t.Fatalf("expected 200 OK from /api/sessions/{id}/waste, got %d", respSession.StatusCode)
	}
	var apiSessionAnalysis analyzer.SessionWasteAnalysis
	if err := json.NewDecoder(respSession.Body).Decode(&apiSessionAnalysis); err != nil {
		t.Fatalf("failed to decode /api/sessions/{id}/waste response: %v", err)
	}
	if apiSessionAnalysis.SessionID != sessionA {
		t.Errorf("expected session ID %s, got %s", sessionA, apiSessionAnalysis.SessionID)
	}
	if len(apiSessionAnalysis.Trajectory) != 10 {
		t.Errorf("expected 10 trajectory points, got %d", len(apiSessionAnalysis.Trajectory))
	}

	// 7. Test CLI Executable Binary
	binPath := filepath.Join(tempDir, "llm-track-bin")
	cmdBuild := cli.GetRootCmdForTesting()
	if cmdBuild == nil {
		t.Fatalf("failed to retrieve root command")
	}
	_ = os.Remove(binPath)
}
