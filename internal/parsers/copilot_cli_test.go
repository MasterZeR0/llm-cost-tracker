package parsers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestCopilotCLIParser_Name(t *testing.T) {
	p, ok := parsers.Get("copilot_cli")
	if !ok || p == nil {
		t.Fatal("expected copilot_cli parser to be registered in init()")
	}
	if p.Name() != "copilot_cli" {
		t.Errorf("expected name copilot_cli, got %s", p.Name())
	}
}

func TestCopilotCLIParser_Parse(t *testing.T) {
	fixturePath := filepath.Join("testdata", "copilot_cli", "history.jsonl")
	p := &parsers.CopilotCLIParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "copilot_cli"}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}

	// Turn 1: explain query
	r1 := reqs[0]
	if r1.Tool != "copilot_cli" {
		t.Errorf("expected tool copilot_cli, got %s", r1.Tool)
	}
	if r1.SessionID != "cli-session-1" {
		t.Errorf("expected session cli-session-1, got %s", r1.SessionID)
	}
	if r1.StepIndex != 1 {
		t.Errorf("expected step index 1, got %d", r1.StepIndex)
	}
	if r1.ID != "copilot_cli:cli-session-1:1" {
		t.Errorf("expected ID copilot_cli:cli-session-1:1, got %s", r1.ID)
	}
	if r1.Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", r1.Model)
	}
	if r1.Status != "estimated" {
		t.Errorf("expected status estimated, got %s", r1.Status)
	}
	if r1.InputTokens <= 0 || r1.OutputTokens <= 0 {
		t.Errorf("expected positive estimated tokens, got (%d, %d)", r1.InputTokens, r1.OutputTokens)
	}
	if r1.CostUSD == nil || *r1.CostUSD <= 0 {
		t.Errorf("expected calculated CostUSD, got %v", r1.CostUSD)
	}

	// Turn 2: suggest query with default model resolution
	r2 := reqs[1]
	if r2.StepIndex != 2 {
		t.Errorf("expected step index 2, got %d", r2.StepIndex)
	}
	if r2.ID != "copilot_cli:cli-session-1:2" {
		t.Errorf("expected ID copilot_cli:cli-session-1:2, got %s", r2.ID)
	}
	if r2.Model != "gpt-4o" {
		t.Errorf("expected model resolved to gpt-4o, got %s", r2.Model)
	}
	if r2.ToolCallCount != 1 {
		t.Errorf("expected 1 tool call for suggest command, got %d", r2.ToolCallCount)
	}
	if len(r2.ToolCalls) != 1 || r2.ToolCalls[0].ToolName != "suggest_command" {
		t.Errorf("expected suggest_command tool call, got %v", r2.ToolCalls)
	}
	if r2.CostUSD == nil || *r2.CostUSD <= 0 {
		t.Errorf("expected calculated CostUSD, got %v", r2.CostUSD)
	}

	// Idempotency: second parse should yield 0 new requests
	reqs2, _, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatalf("Second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on second parse, got %d", len(reqs2))
	}
}

func TestCopilotCLIParser_TruncatedReset(t *testing.T) {
	fixturePath := filepath.Join("testdata", "copilot_cli", "history.jsonl")
	p := &parsers.CopilotCLIParser{}

	state := store.SyncState{
		SourcePath: fixturePath,
		Tool:       "copilot_cli",
		LastOffset: 9999999,
	}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
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

func TestCopilotCLIParser_Discover(t *testing.T) {
	dir := t.TempDir()
	historyPath := filepath.Join(dir, "history.jsonl")
	if err := os.WriteFile(historyPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.CopilotCLIParser{}
	sources, err := p.Discover(config.ToolConfig{
		Enabled: true,
		Paths:   []string{dir},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}

	if len(sources) != 1 {
		t.Fatalf("expected 1 source discovered, got %d", len(sources))
	}
}

func TestCopilotCLIParser_ParseFormattedJSONArray(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "history.json")

	content := `[
		{
			"session_id": "cli-arr-session",
			"timestamp": "2026-10-18T10:00:00Z",
			"command_type": "explain",
			"query": "explain git rebase",
			"explanation": "git rebase reapplies commits on top of another base tip.",
			"model": "gpt-4o"
		},
		{
			"session_id": "cli-arr-session",
			"timestamp": "2026-10-18T10:05:00Z",
			"command_type": "suggest",
			"query": "list docker containers",
			"suggestion": "docker ps -a",
			"model": "gpt-4o"
		}
	]`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.CopilotCLIParser{}
	state := store.SyncState{SourcePath: jsonPath, Tool: "copilot_cli"}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests from formatted JSON array, got %d", len(reqs))
	}

	r1 := reqs[0]
	if r1.SessionID != "cli-arr-session" {
		t.Errorf("expected session cli-arr-session, got %s", r1.SessionID)
	}
	if r1.StepIndex != 1 {
		t.Errorf("expected step index 1, got %d", r1.StepIndex)
	}
	if r1.InputTokens <= 0 || r1.OutputTokens <= 0 {
		t.Errorf("expected positive estimated tokens, got (%d, %d)", r1.InputTokens, r1.OutputTokens)
	}
	if r1.CostUSD == nil || *r1.CostUSD <= 0 {
		t.Errorf("expected positive CostUSD, got %v", r1.CostUSD)
	}

	r2 := reqs[1]
	if r2.StepIndex != 2 {
		t.Errorf("expected step index 2, got %d", r2.StepIndex)
	}
	if r2.ToolCallCount != 1 {
		t.Errorf("expected 1 tool call for suggest command, got %d", r2.ToolCallCount)
	}

	// Idempotency: second parse should return 0 requests
	reqs2, _, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, newState)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on second parse, got %d", len(reqs2))
	}
}

func TestCopilotCLIParser_ParseFormattedJSONSingle(t *testing.T) {
	dir := t.TempDir()
	jsonPath := filepath.Join(dir, "single.json")

	content := `{
		"session_id": "single-cli-session",
		"timestamp": "2026-10-18T11:00:00Z",
		"command_type": "suggest",
		"query": "find files larger than 100M",
		"suggestion": "find . -type f -size +100M",
		"model": "gpt-4o"
	}`
	if err := os.WriteFile(jsonPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.CopilotCLIParser{}
	state := store.SyncState{SourcePath: jsonPath, Tool: "copilot_cli"}
	reqs, newState, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse single JSON failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].SessionID != "single-cli-session" {
		t.Errorf("expected session single-cli-session, got %s", reqs[0].SessionID)
	}
	if reqs[0].StepIndex != 1 {
		t.Errorf("expected step index 1, got %d", reqs[0].StepIndex)
	}
	if reqs[0].ToolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", reqs[0].ToolCallCount)
	}

	// Idempotency
	reqs2, _, err := p.Parse(parsers.DataSource{Path: jsonPath, Type: "json"}, newState)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs2))
	}
}
