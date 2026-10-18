package parsers

import (
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestRooCodeParser_Name(t *testing.T) {
	p, ok := Get("roo_code")
	if !ok || p == nil {
		t.Fatal("expected roo_code parser to be registered in init()")
	}
	if p.Name() != "roo_code" {
		t.Errorf("expected name roo_code, got %s", p.Name())
	}
}

func TestRooCodeParser_Discover(t *testing.T) {
	p := &RooCodeParser{}
	taskDir := filepath.Join("testdata", "roo_code", "tasks")
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

func TestRooCodeParser_Parse(t *testing.T) {
	fixturePath := filepath.Join("testdata", "roo_code", "tasks", "task-roo-01", "api_conversation_history.json")
	p := &RooCodeParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "roo_code"}
	reqs, newState, err := p.Parse(DataSource{Path: fixturePath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) == 0 {
		t.Fatalf("expected parsed requests, got 0")
	}

	req := reqs[0]
	if req.Tool != "roo_code" {
		t.Errorf("expected tool roo_code, got %s", req.Tool)
	}
	if req.SessionID != "task-roo-01" {
		t.Errorf("expected session task-roo-01, got %s", req.SessionID)
	}
	if req.InputTokens != 2000 {
		t.Errorf("expected 2000 input tokens, got %d", req.InputTokens)
	}
	if req.OutputTokens != 400 {
		t.Errorf("expected 400 output tokens, got %d", req.OutputTokens)
	}
	if req.CacheReadTokens != 3000 {
		t.Errorf("expected 3000 cache read tokens, got %d", req.CacheReadTokens)
	}
	if req.CacheWriteTokens != 4000 {
		t.Errorf("expected 4000 cache write tokens, got %d", req.CacheWriteTokens)
	}
	if req.TotalTokens != 2400 {
		t.Errorf("expected 2400 total tokens, got %d", req.TotalTokens)
	}
	if req.CostUSD == nil || *req.CostUSD <= 0 {
		t.Errorf("expected positive CostUSD, got %v", req.CostUSD)
	}
	if req.ToolCallCount == 0 {
		t.Errorf("expected tool calls detected, got %d", req.ToolCallCount)
	}
	if len(req.ToolCalls) == 0 || req.ToolCalls[0].ToolName != "read_file" {
		t.Errorf("expected read_file tool call, got %v", req.ToolCalls)
	}
	if req.ID != "roo_code:task-roo-01:0" {
		t.Errorf("expected deterministic ID roo_code:task-roo-01:0, got %s", req.ID)
	}
	if req.Status != "success" {
		t.Errorf("expected status success, got %s", req.Status)
	}

	// Verify second turn
	if len(reqs) > 1 {
		req2 := reqs[1]
		if req2.CacheReadTokens != 6000 {
			t.Errorf("expected 6000 cache read tokens on turn 2, got %d", req2.CacheReadTokens)
		}
		if req2.StepIndex != 1 {
			t.Errorf("expected step_index 1 on turn 2, got %d", req2.StepIndex)
		}
		if len(req2.ToolCalls) == 0 || req2.ToolCalls[0].ToolName != "write_to_file" {
			t.Errorf("expected write_to_file tool call on turn 2, got %v", req2.ToolCalls)
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

func TestRooCodeParser_IndependentFromCline(t *testing.T) {
	clineParser, okCline := Get("cline")
	rooParser, okRoo := Get("roo_code")

	if !okCline || clineParser == nil {
		t.Fatal("cline parser should be registered")
	}
	if !okRoo || rooParser == nil {
		t.Fatal("roo_code parser should be registered")
	}
	if clineParser.Name() == rooParser.Name() {
		t.Errorf("parsers should have distinct names: %s vs %s", clineParser.Name(), rooParser.Name())
	}
	if clineParser == rooParser {
		t.Errorf("parsers should be distinct instances")
	}
}
