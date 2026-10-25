package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/cli"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func ptrFloat64(f float64) *float64 {
	return &f
}

func ptrInt64(i int64) *int64 {
	return &i
}

func TestRenderWaste_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "waste_empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var buf bytes.Buffer
	if err := cli.RenderWaste(&buf, st, "today"); err != nil {
		t.Fatalf("unexpected error rendering waste: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "=== LLM Cost Tracker - Context Bloat & Waste Diagnostics ===") {
		t.Errorf("expected header in output: %s", out)
	}
	if !strings.Contains(out, "Period: today | Sessions Analyzed: 0 | Flagged: 0 (0.0%) | Avg Health: 100.0/100") {
		t.Errorf("expected period and 0 sessions in header: %s", out)
	}
	if !strings.Contains(out, "Estimated Avoidable Waste: $0.00 (0 tokens)") {
		t.Errorf("expected estimated avoidable waste in header: %s", out)
	}
	if !strings.Contains(out, "No sessions found for period today.") {
		t.Errorf("expected 'No sessions found for period today.' message: %s", out)
	}
}

func TestRenderWaste_CleanSessions(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "waste_clean.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	reqs := []store.Request{
		{
			ID:              "req-1",
			SessionID:       "sess-clean-1",
			Tool:            "claude-code",
			Timestamp:       now.Add(-2 * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       1,
			InputTokens:     2000,
			CacheReadTokens: 10000,
			OutputTokens:    500,
			TotalTokens:     12500,
			CostUSD:         ptrFloat64(0.015),
			Status:          "success",
		},
		{
			ID:              "req-2",
			SessionID:       "sess-clean-1",
			Tool:            "claude-code",
			Timestamp:       now.Add(-1 * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       2,
			InputTokens:     2200,
			CacheReadTokens: 10000,
			OutputTokens:    400,
			TotalTokens:     12600,
			CostUSD:         ptrFloat64(0.015),
			Status:          "success",
		},
	}
	if _, err := st.InsertRequests(reqs); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.RenderWaste(&buf, st, "today"); err != nil {
		t.Fatalf("unexpected error rendering waste: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Sessions Analyzed: 1") {
		t.Errorf("expected 'Sessions Analyzed: 1' in output: %s", out)
	}
	if !strings.Contains(out, "Flagged: 0 (0.0%)") {
		t.Errorf("expected 'Flagged: 0 (0.0%%)' in output: %s", out)
	}
	if !strings.Contains(out, "Avg Health: 100.0/100") {
		t.Errorf("expected 'Avg Health: 100.0/100' in output: %s", out)
	}
	if !strings.Contains(out, "No context bloat or token waste detected for period today. All sessions healthy!") {
		t.Errorf("expected all healthy message in output: %s", out)
	}
}

func TestRenderWaste_WasteSessions(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "waste_dirty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()

	// 1. Clean session
	cleanReqs := []store.Request{
		{
			ID:              "req-c1",
			SessionID:       "sess-clean",
			Tool:            "claude-code",
			Timestamp:       now.Add(-10 * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       1,
			InputTokens:     2000,
			CacheReadTokens: 10000,
			OutputTokens:    500,
			TotalTokens:     12500,
			CostUSD:         ptrFloat64(0.015),
			Status:          "success",
		},
		{
			ID:              "req-c2",
			SessionID:       "sess-clean",
			Tool:            "claude-code",
			Timestamp:       now.Add(-9 * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       2,
			InputTokens:     2100,
			CacheReadTokens: 10000,
			OutputTokens:    400,
			TotalTokens:     12500,
			CostUSD:         ptrFloat64(0.015),
			Status:          "success",
		},
	}

	// 2. Waste session: Consecutive tool error loop (3 turns of errors)
	wasteReqs := []store.Request{
		{
			ID:             "req-w1",
			SessionID:      "sess-error-loop",
			Tool:           "agy",
			Timestamp:      now.Add(-5 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      1,
			InputTokens:    50000,
			OutputTokens:   2000,
			TotalTokens:    52000,
			CostUSD:        ptrFloat64(0.25),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-w1",
					RequestID: "req-w1",
					SessionID: "sess-error-loop",
					ToolName:  "run_tests",
					Status:    "error",
					CreatedAt: now.Add(-5 * time.Minute),
				},
			},
		},
		{
			ID:             "req-w2",
			SessionID:      "sess-error-loop",
			Tool:           "agy",
			Timestamp:      now.Add(-4 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      2,
			InputTokens:    50000,
			OutputTokens:   2000,
			TotalTokens:    52000,
			CostUSD:        ptrFloat64(0.25),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-w2",
					RequestID: "req-w2",
					SessionID: "sess-error-loop",
					ToolName:  "run_tests",
					Status:    "error",
					CreatedAt: now.Add(-4 * time.Minute),
				},
			},
		},
		{
			ID:             "req-w3",
			SessionID:      "sess-error-loop",
			Tool:           "agy",
			Timestamp:      now.Add(-3 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      3,
			InputTokens:    50000,
			OutputTokens:   2000,
			TotalTokens:    52000,
			CostUSD:        ptrFloat64(0.25),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-w3",
					RequestID: "req-w3",
					SessionID: "sess-error-loop",
					ToolName:  "run_tests",
					Status:    "error",
					CreatedAt: now.Add(-3 * time.Minute),
				},
			},
		},
	}

	if _, err := st.InsertRequests(append(cleanReqs, wasteReqs...)); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.RenderWaste(&buf, st, "today"); err != nil {
		t.Fatalf("unexpected error rendering waste: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Sessions Analyzed: 2") {
		t.Errorf("expected 'Sessions Analyzed: 2' in output: %s", out)
	}
	if !strings.Contains(out, "Flagged: 1 (50.0%)") {
		t.Errorf("expected 'Flagged: 1 (50.0%%)' in output: %s", out)
	}
	if !strings.Contains(out, "RANK") || !strings.Contains(out, "PRIMARY ISSUE") {
		t.Errorf("expected table columns in output: %s", out)
	}
	if !strings.Contains(out, "sess-error-loop") {
		t.Errorf("expected 'sess-error-loop' in table: %s", out)
	}
	if !strings.Contains(out, "Tool Error Loop") {
		t.Errorf("expected 'Tool Error Loop' as primary issue in output: %s", out)
	}
	if !strings.Contains(out, "Recommendations:") {
		t.Errorf("expected 'Recommendations:' section in output: %s", out)
	}
}

func TestRenderWaste_InvalidPeriod(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "waste_invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var buf bytes.Buffer
	err = cli.RenderWaste(&buf, st, "invalid_period")
	if err == nil {
		t.Fatalf("expected error for invalid period, got nil")
	}
	if !strings.Contains(err.Error(), "invalid period \"invalid_period\"") {
		t.Errorf("expected invalid period error message, got: %v", err)
	}
}

func TestRenderSessionDetail_WasteDiagnostics(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "session_waste.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	sessID := "sess-waste-detail"

	// 3 consecutive tool failure turns -> error loop
	turns := []store.Request{
		{
			ID:             "req-d1",
			SessionID:      sessID,
			Tool:           "agy",
			Timestamp:      now.Add(-3 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      1,
			InputTokens:    40000,
			OutputTokens:   1000,
			TotalTokens:    41000,
			CostUSD:        ptrFloat64(0.15),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-d1",
					RequestID: "req-d1",
					SessionID: sessID,
					ToolName:  "compile",
					Status:    "error",
					CreatedAt: now.Add(-3 * time.Minute),
				},
			},
		},
		{
			ID:             "req-d2",
			SessionID:      sessID,
			Tool:           "agy",
			Timestamp:      now.Add(-2 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      2,
			InputTokens:    40000,
			OutputTokens:   1000,
			TotalTokens:    41000,
			CostUSD:        ptrFloat64(0.15),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-d2",
					RequestID: "req-d2",
					SessionID: sessID,
					ToolName:  "compile",
					Status:    "error",
					CreatedAt: now.Add(-2 * time.Minute),
				},
			},
		},
		{
			ID:             "req-d3",
			SessionID:      sessID,
			Tool:           "agy",
			Timestamp:      now.Add(-1 * time.Minute),
			Model:          "gemini-2-pro",
			StepIndex:      3,
			InputTokens:    40000,
			OutputTokens:   1000,
			TotalTokens:    41000,
			CostUSD:        ptrFloat64(0.15),
			Status:         "error",
			ToolCallCount:  1,
			ToolErrorCount: 1,
			ToolCalls: []store.ToolCall{
				{
					ID:        "tc-d3",
					RequestID: "req-d3",
					SessionID: sessID,
					ToolName:  "compile",
					Status:    "error",
					CreatedAt: now.Add(-1 * time.Minute),
				},
			},
		},
	}

	if _, err := st.InsertRequests(turns); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.RenderSessionDetail(&buf, st, sessID); err != nil {
		t.Fatalf("unexpected error rendering session detail: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "Context Health:") {
		t.Errorf("expected 'Context Health:' in detail output: %s", out)
	}
	if !strings.Contains(out, "(FLAGGED FOR WASTE)") {
		t.Errorf("expected '(FLAGGED FOR WASTE)' in detail output: %s", out)
	}
	if !strings.Contains(out, "Waste USD:") {
		t.Errorf("expected 'Waste USD:' in detail output: %s", out)
	}
	if !strings.Contains(out, "Findings:") {
		t.Errorf("expected 'Findings:' in detail output: %s", out)
	}
	if !strings.Contains(out, "Turn 1:") {
		t.Errorf("expected 'Turn 1:' in detail output: %s", out)
	}
	if !strings.Contains(out, "->") {
		t.Errorf("expected '->' recommendation separator in detail output: %s", out)
	}
}

func TestRenderSessionDetail_CleanSessionNoWaste(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "session_clean.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	now := time.Now().UTC()
	sessID := "sess-clean-detail"

	turns := []store.Request{
		{
			ID:              "req-c1",
			SessionID:       sessID,
			Tool:            "claude-code",
			Timestamp:       now.Add(-2 * time.Minute),
			Model:           "claude-3-5-sonnet",
			StepIndex:       1,
			InputTokens:     2000,
			CacheReadTokens: 10000,
			OutputTokens:    500,
			TotalTokens:     12500,
			CostUSD:         ptrFloat64(0.015),
			Status:          "success",
		},
	}

	if _, err := st.InsertRequests(turns); err != nil {
		t.Fatal(err)
	}

	var buf bytes.Buffer
	if err := cli.RenderSessionDetail(&buf, st, sessID); err != nil {
		t.Fatalf("unexpected error rendering session detail: %v", err)
	}

	out := buf.String()
	if strings.Contains(out, "FLAGGED FOR WASTE") {
		t.Errorf("expected clean session NOT to have '(FLAGGED FOR WASTE)': %s", out)
	}
	if strings.Contains(out, "Findings:") {
		t.Errorf("expected clean session NOT to have 'Findings:': %s", out)
	}
}

func TestWasteCmd_Execute(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "waste_cmd.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cli.SetStoreForTesting(st)

	var buf bytes.Buffer
	rootCmd := cli.GetRootCmdForTesting()
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"waste", "--period=today"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("unexpected error executing waste cmd via rootCmd: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "=== LLM Cost Tracker - Context Bloat & Waste Diagnostics ===") {
		t.Errorf("expected diagnostics header: %s", out)
	}
	if !strings.Contains(out, "No sessions found for period today.") {
		t.Errorf("expected empty period message: %s", out)
	}
}
