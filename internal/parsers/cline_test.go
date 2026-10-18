package parsers

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestClineParser_Name(t *testing.T) {
	p, ok := Get("cline")
	if !ok || p == nil {
		t.Fatal("expected cline parser to be registered in init()")
	}
	if p.Name() != "cline" {
		t.Errorf("expected name cline, got %s", p.Name())
	}
}

func TestClineParser_Discover(t *testing.T) {
	p := &ClineParser{}
	taskDir := filepath.Join("testdata", "cline", "tasks")
	cfg := config.ToolConfig{
		Enabled: true,
		Paths:   []string{taskDir},
	}

	sources, err := p.Discover(cfg)
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(sources) == 0 {
		t.Fatalf("expected at least 1 discovered source, got 0")
	}

	found := false
	for _, s := range sources {
		if filepath.Base(s.Path) == "api_conversation_history.json" {
			found = true
			if s.Type != "json" {
				t.Errorf("expected type json, got %s", s.Type)
			}
		}
	}
	if !found {
		t.Errorf("expected to find api_conversation_history.json in sources: %v", sources)
	}
}

func TestClineParser_Parse(t *testing.T) {
	fixturePath := filepath.Join("testdata", "cline", "tasks", "task-001", "api_conversation_history.json")
	p := &ClineParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "cline"}
	reqs, newState, err := p.Parse(DataSource{Path: fixturePath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) == 0 {
		t.Fatalf("expected parsed requests, got 0")
	}

	req := reqs[0]
	if req.Tool != "cline" {
		t.Errorf("expected tool cline, got %s", req.Tool)
	}
	if req.SessionID != "task-001" {
		t.Errorf("expected session task-001, got %s", req.SessionID)
	}
	if req.Model != "claude-3-5-sonnet-20241022" {
		t.Errorf("expected model claude-3-5-sonnet-20241022, got %s", req.Model)
	}
	if req.InputTokens != 1200 {
		t.Errorf("expected 1200 input tokens, got %d", req.InputTokens)
	}
	if req.OutputTokens != 350 {
		t.Errorf("expected 350 output tokens, got %d", req.OutputTokens)
	}
	if req.CacheReadTokens != 2500 {
		t.Errorf("expected 2500 cache read tokens, got %d", req.CacheReadTokens)
	}
	if req.CacheWriteTokens != 5000 {
		t.Errorf("expected 5000 cache write tokens, got %d", req.CacheWriteTokens)
	}
	if req.TotalTokens != 1550 {
		t.Errorf("expected 1550 total tokens, got %d", req.TotalTokens)
	}
	if req.CostUSD == nil || *req.CostUSD <= 0 {
		t.Errorf("expected positive CostUSD, got %v", req.CostUSD)
	}
	if req.ToolCallCount == 0 {
		t.Errorf("expected tool calls detected, got %d", req.ToolCallCount)
	}
	if len(req.ToolCalls) == 0 || req.ToolCalls[0].ToolName != "execute_command" {
		t.Errorf("expected execute_command tool call, got %v", req.ToolCalls)
	}
	if req.ID != "cline:task-001:0" {
		t.Errorf("expected deterministic ID cline:task-001:0, got %s", req.ID)
	}
	if req.Status != "success" {
		t.Errorf("expected status success, got %s", req.Status)
	}

	// Verify second turn
	if len(reqs) > 1 {
		req2 := reqs[1]
		if req2.CacheReadTokens != 6200 {
			t.Errorf("expected 6200 cache read tokens on turn 2, got %d", req2.CacheReadTokens)
		}
		if req2.StepIndex != 1 {
			t.Errorf("expected step_index 1 on turn 2, got %d", req2.StepIndex)
		}
	}

	// Test idempotency
	reqs2, _, err := p.Parse(DataSource{Path: fixturePath, Type: "json"}, newState)
	if err != nil {
		t.Fatalf("Second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on second parse, got %d", len(reqs2))
	}
}

func TestClineParser_Truncation(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "task-trunc")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	histFile := filepath.Join(taskDir, "api_conversation_history.json")

	content := `[
  {
    "role": "assistant",
    "content": "Short test",
    "model": "claude-3-5-sonnet-20241022",
    "usage": { "input_tokens": 100, "output_tokens": 50 }
  }
]`
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &ClineParser{}
	// State with LastOffset much larger than file size simulates truncation/rotation
	staleState := store.SyncState{
		SourcePath: histFile,
		Tool:       "cline",
		LastOffset: 999999,
	}

	reqs, newState, err := p.Parse(DataSource{Path: histFile, Type: "json"}, staleState)
	if err != nil {
		t.Fatalf("Parse on truncated file failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request after offset reset, got %d", len(reqs))
	}
	if newState.LastOffset <= 0 {
		t.Errorf("expected LastOffset to be reset to file size, got %d", newState.LastOffset)
	}
}

func TestClineParser_ToolError(t *testing.T) {
	dir := t.TempDir()
	taskDir := filepath.Join(dir, "task-err")
	if err := os.MkdirAll(taskDir, 0755); err != nil {
		t.Fatal(err)
	}
	histFile := filepath.Join(taskDir, "api_conversation_history.json")

	content := `[
  {
    "role": "assistant",
    "content": "<execute_command>cat missing.txt</execute_command>",
    "model": "claude-3-5-sonnet-20241022",
    "usage": { "input_tokens": 500, "output_tokens": 100 }
  },
  {
    "role": "user",
    "content": "[execute_command for 'cat missing.txt'] Result:\nError: file not found (exit code 1)"
  }
]`
	if err := os.WriteFile(histFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &ClineParser{}
	reqs, _, err := p.Parse(DataSource{Path: histFile, Type: "json"}, store.SyncState{SourcePath: histFile, Tool: "cline"})
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error, got %d", reqs[0].ToolErrorCount)
	}
	if len(reqs[0].ToolCalls) > 0 && reqs[0].ToolCalls[0].Status != "error" {
		t.Errorf("expected tool call status error, got %s", reqs[0].ToolCalls[0].Status)
	}
}
