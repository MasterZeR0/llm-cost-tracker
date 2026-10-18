package parsers_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestWindsurfParser_Name(t *testing.T) {
	p, ok := parsers.Get("windsurf")
	if !ok || p == nil {
		t.Fatal("expected windsurf parser to be registered in init()")
	}
	if p.Name() != "windsurf" {
		t.Errorf("expected name windsurf, got %s", p.Name())
	}
}

func TestWindsurfParser_ParseCascadeSession(t *testing.T) {
	fixturePath := filepath.Join("testdata", "windsurf", "cascade", "session-001.json")
	p := &parsers.WindsurfParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "windsurf"}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}

	// Turn 1: native tokens
	r1 := reqs[0]
	if r1.Tool != "windsurf" {
		t.Errorf("expected tool windsurf, got %s", r1.Tool)
	}
	if r1.SessionID != "session-001" {
		t.Errorf("expected session session-001, got %s", r1.SessionID)
	}
	if r1.StepIndex != 1 {
		t.Errorf("expected step index 1, got %d", r1.StepIndex)
	}
	if r1.ID != "windsurf:session-001:1" {
		t.Errorf("expected ID windsurf:session-001:1, got %s", r1.ID)
	}
	if r1.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model claude-3-5-sonnet, got %s", r1.Model)
	}
	if r1.InputTokens != 850 || r1.OutputTokens != 120 {
		t.Errorf("expected tokens (850, 120), got (%d, %d)", r1.InputTokens, r1.OutputTokens)
	}
	if r1.TotalTokens != 970 {
		t.Errorf("expected total tokens 970, got %d", r1.TotalTokens)
	}
	if r1.Status != "success" {
		t.Errorf("expected status success, got %s", r1.Status)
	}
	if r1.LatencyMS == nil || *r1.LatencyMS != 1250 {
		t.Errorf("expected latency 1250, got %v", r1.LatencyMS)
	}
	if r1.ToolCallCount != 1 || r1.ToolErrorCount != 0 {
		t.Errorf("expected 1 tool call, 0 errors, got (%d, %d)", r1.ToolCallCount, r1.ToolErrorCount)
	}
	if len(r1.ToolCalls) != 1 || r1.ToolCalls[0].ToolName != "search_codebase" {
		t.Errorf("expected search_codebase tool call, got %v", r1.ToolCalls)
	}
	if r1.CostUSD == nil || *r1.CostUSD <= 0 {
		t.Errorf("expected calculated CostUSD, got %v", r1.CostUSD)
	}

	// Turn 2: estimated tokens and tool error
	r2 := reqs[1]
	if r2.StepIndex != 2 {
		t.Errorf("expected step index 2, got %d", r2.StepIndex)
	}
	if r2.ID != "windsurf:session-001:2" {
		t.Errorf("expected ID windsurf:session-001:2, got %s", r2.ID)
	}
	if r2.Status != "estimated" {
		t.Errorf("expected status estimated, got %s", r2.Status)
	}
	if r2.InputTokens <= 0 || r2.OutputTokens <= 0 {
		t.Errorf("expected positive estimated tokens, got (%d, %d)", r2.InputTokens, r2.OutputTokens)
	}
	if r2.LatencyMS == nil || *r2.LatencyMS != 2300 {
		t.Errorf("expected latency 2300, got %v", r2.LatencyMS)
	}
	if r2.ToolCallCount != 1 || r2.ToolErrorCount != 1 {
		t.Errorf("expected 1 tool call, 1 error, got (%d, %d)", r2.ToolCallCount, r2.ToolErrorCount)
	}
	if len(r2.ToolCalls) != 1 || r2.ToolCalls[0].ToolName != "run_command" || r2.ToolCalls[0].Status != "error" {
		t.Errorf("expected run_command error tool call, got %v", r2.ToolCalls)
	}

	// Idempotency: second parse with newState should yield 0 new requests
	reqs2, _, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "json"}, newState)
	if err != nil {
		t.Fatalf("Second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on second parse, got %d", len(reqs2))
	}
}

func TestWindsurfParser_TruncatedReset(t *testing.T) {
	fixturePath := filepath.Join("testdata", "windsurf", "cascade", "session-001.json")
	p := &parsers.WindsurfParser{}

	// If LastOffset exceeds file size, it should reset offset to 0 and re-parse
	state := store.SyncState{
		SourcePath: fixturePath,
		Tool:       "windsurf",
		LastOffset: 9999999,
	}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed after truncation: %v", err)
	}
	if len(reqs) != 2 {
		t.Errorf("expected 2 requests after reset, got %d", len(reqs))
	}
	if newState.LastOffset <= 0 {
		t.Errorf("expected positive new offset, got %d", newState.LastOffset)
	}
}

func TestWindsurfParser_ParseVSCDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create sqlite db: %v", err)
	}

	cascadeJSON := `{
		"session_id": "vscdb-session-1",
		"model": "claude-3-5-sonnet",
		"steps": [
			{
				"step_index": 1,
				"timestamp": "2026-10-18T10:00:00Z",
				"user_prompt": "Explain this code",
				"assistant_response": "This code implements a tokenizer.",
				"tokens": {"input_tokens": 100, "output_tokens": 50}
			}
		]
	}`

	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('cascade.sessions', ?);
	`, cascadeJSON)
	db.Close()
	if err != nil {
		t.Fatalf("failed to insert mock data: %v", err)
	}

	p := &parsers.WindsurfParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "windsurf"}
	reqs, _, err := p.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse vscdb failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request from vscdb, got %d", len(reqs))
	}
	if reqs[0].SessionID != "vscdb-session-1" {
		t.Errorf("expected session vscdb-session-1, got %s", reqs[0].SessionID)
	}
	if reqs[0].ID != "windsurf:vscdb-session-1:1" {
		t.Errorf("expected ID windsurf:vscdb-session-1:1, got %s", reqs[0].ID)
	}
	if reqs[0].InputTokens != 100 || reqs[0].OutputTokens != 50 {
		t.Errorf("tokens mismatch: (%d, %d)", reqs[0].InputTokens, reqs[0].OutputTokens)
	}
}

func TestWindsurfParser_Discover(t *testing.T) {
	dir := t.TempDir()
	wsDir := filepath.Join(dir, "workspaceStorage", "workspace-1")
	cascadeDir := filepath.Join(dir, "cascade")
	if err := os.MkdirAll(wsDir, 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.MkdirAll(cascadeDir, 0755); err != nil {
		t.Fatal(err)
	}

	// Create dummy state.vscdb with ItemTable
	vscdbPath := filepath.Join(wsDir, "state.vscdb")
	db, err := sql.Open("sqlite3", vscdbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec("CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT)")
	db.Close()

	// Create dummy json
	jsonPath := filepath.Join(cascadeDir, "session-002.json")
	if err := os.WriteFile(jsonPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.WindsurfParser{}
	sources, err := p.Discover(config.ToolConfig{
		Enabled: true,
		Paths:   []string{dir},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(sources) != 2 {
		t.Fatalf("expected 2 sources discovered, got %d", len(sources))
	}
}

func TestWindsurfParser_PartialWriteResilience(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "active_session.json")

	// 1. Simulate partial/incomplete write during active typing/streaming
	if err := os.WriteFile(jsonPath, []byte(`{"session_id": "ws-partial", "steps": [{"step_index": 1`), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.WindsurfParser{}
	initialState := store.SyncState{SourcePath: jsonPath, Tool: "windsurf", LastOffset: 0}

	reqs1, newState1, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, initialState)
	if err == nil {
		t.Fatal("expected error on unmarshalling partial JSON, got nil")
	}
	if len(reqs1) != 0 {
		t.Fatalf("expected 0 requests from partial JSON, got %d", len(reqs1))
	}
	if newState1.LastOffset != 0 {
		t.Fatalf("expected LastOffset to remain 0 after partial write error, got %d", newState1.LastOffset)
	}

	// 2. Simulate writer completing the JSON file
	completeJSON := `{
		"session_id": "ws-partial",
		"steps": [
			{
				"step_index": 1,
				"user_prompt": "hello",
				"assistant_response": "world",
				"tokens": { "input_tokens": 100, "output_tokens": 50 }
			}
		]
	}`
	if err := os.WriteFile(jsonPath, []byte(completeJSON), 0644); err != nil {
		t.Fatal(err)
	}

	reqs2, newState2, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, newState1)
	if err != nil {
		t.Fatalf("Parse on completed JSON failed: %v", err)
	}
	if len(reqs2) != 1 {
		t.Fatalf("expected 1 request after complete write, got %d", len(reqs2))
	}
	if reqs2[0].ID != "windsurf:ws-partial:1" {
		t.Errorf("expected ID windsurf:ws-partial:1, got %s", reqs2[0].ID)
	}
	if newState2.LastOffset <= 0 {
		t.Errorf("expected positive LastOffset after successful parse, got %d", newState2.LastOffset)
	}
}
