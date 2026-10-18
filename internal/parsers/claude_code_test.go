package parsers_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestClaudeCodeParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/claude_sample.jsonl")
	parser := &parsers.ClaudeCodeParser{}

	state := store.SyncState{
		SourcePath: fixturePath,
		Tool:       "claude_code",
		LastOffset: 0,
	}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("expected 2 assistant turns, got %d", len(reqs))
	}

	first := reqs[0]
	if first.Tool != "claude_code" {
		t.Errorf("expected tool claude_code, got %s", first.Tool)
	}
	if first.Model != "claude-3-7-sonnet" {
		t.Errorf("expected claude-3-7-sonnet, got %s", first.Model)
	}
	if first.InputTokens != 1200 || first.OutputTokens != 85 {
		t.Errorf("tokens mismatch: in=%d, out=%d", first.InputTokens, first.OutputTokens)
	}
	if first.CostUSD == nil || *first.CostUSD != 0.0125 {
		t.Errorf("cost mismatch: %v", first.CostUSD)
	}
	if first.CacheReadTokens != 300 {
		t.Errorf("cache read mismatch: %d", first.CacheReadTokens)
	}
	if newState.LastOffset <= 0 {
		t.Errorf("expected positive offset advance, got %d", newState.LastOffset)
	}

	// Incremental test from the new offset
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Fatalf("expected 0 new requests from end of file, got %d", len(reqs2))
	}
}

func TestClaudeCodeParser_Name(t *testing.T) {
	p := &parsers.ClaudeCodeParser{}
	if p.Name() != "claude_code" {
		t.Errorf("expected claude_code, got %s", p.Name())
	}
}

func TestClaudeCodeParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	p1 := filepath.Join(tmpDir, "session1.jsonl")
	p2 := filepath.Join(tmpDir, "sub", "session2.jsonl")
	pTxt := filepath.Join(tmpDir, "notes.txt")

	if err := os.MkdirAll(filepath.Dir(p2), 0755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p1, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(p2, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(pTxt, []byte("ignore me"), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.ClaudeCodeParser{}
	cfg := config.ToolConfig{
		Paths: []string{tmpDir},
	}

	sources, err := parser.Discover(cfg)
	if err != nil {
		t.Fatalf("unexpected error during Discover: %v", err)
	}

	if len(sources) != 2 {
		t.Fatalf("expected 2 jsonl sources, got %d", len(sources))
	}
	for _, s := range sources {
		if s.Type != "jsonl" {
			t.Errorf("expected type jsonl, got %s", s.Type)
		}
		if filepath.Ext(s.Path) != ".jsonl" {
			t.Errorf("expected .jsonl extension, got %s", s.Path)
		}
	}
}

func TestClaudeCodeParser_TruncationAndIncompleteLine(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "test_session.jsonl")

	line1 := `{"type":"assistant","costUSD":0.005,"durationMs":500,"message":{"model":"claude-3-haiku","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(logFile, []byte(line1), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.ClaudeCodeParser{}
	state := store.SyncState{
		SourcePath: logFile,
		Tool:       "claude_code",
		LastOffset: 0,
	}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if newState.LastOffset != int64(len(line1)) {
		t.Fatalf("expected offset %d, got %d", len(line1), newState.LastOffset)
	}

	// Now append an incomplete line (no trailing newline)
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	incompleteLine := `{"type":"assistant","costUSD":0.00`
	_, err = f.WriteString(incompleteLine)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	// Parsing with newState should return 0 requests and NOT advance offset past incomplete line
	reqs, afterIncompleteState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Fatalf("expected 0 requests from incomplete line, got %d", len(reqs))
	}
	if afterIncompleteState.LastOffset != newState.LastOffset {
		t.Fatalf("expected offset to remain %d, got %d", newState.LastOffset, afterIncompleteState.LastOffset)
	}

	// Truncation test: overwrite file with shorter content, state.LastOffset > new file size
	truncatedContent := `{"type":"assistant","costUSD":0.010,"durationMs":800,"timestamp":"2026-10-17T12:00:00Z","message":{"model":"claude-3-opus","role":"assistant","usage":{"input_tokens":200,"output_tokens":100,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(logFile, []byte(truncatedContent), 0644); err != nil {
		t.Fatal(err)
	}

	// LastOffset is now beyond the truncated file size
	staleState := store.SyncState{
		SourcePath: logFile,
		Tool:       "claude_code",
		LastOffset: 99999,
	}
	reqsReset, resetState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, staleState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqsReset) != 1 {
		t.Fatalf("expected 1 request after truncation reset, got %d", len(reqsReset))
	}
	if reqsReset[0].Model != "claude-3-opus" {
		t.Errorf("expected claude-3-opus, got %s", reqsReset[0].Model)
	}
	expectedTime, _ := time.Parse(time.RFC3339, "2026-10-17T12:00:00Z")
	if !reqsReset[0].Timestamp.Equal(expectedTime) {
		t.Errorf("expected timestamp %v, got %v", expectedTime, reqsReset[0].Timestamp)
	}
	if resetState.LastOffset != int64(len(truncatedContent)) {
		t.Errorf("expected offset %d, got %d", len(truncatedContent), resetState.LastOffset)
	}
}

func TestClaudeCodeParser_NonExistentFile(t *testing.T) {
	parser := &parsers.ClaudeCodeParser{}
	state := store.SyncState{
		SourcePath: "/path/to/nonexistent/file.jsonl",
		Tool:       "claude_code",
		LastOffset: 0,
	}
	_, _, err := parser.Parse(parsers.DataSource{Path: state.SourcePath, Type: "jsonl"}, state)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestClaudeCodeParser_InvalidJSON(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "invalid.jsonl")

	content := "not a json line\n" +
		`{"type":"assistant","costUSD":0.005,"durationMs":500,"message":{"model":"claude-3-haiku","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n" +
		"another corrupt line\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.ClaudeCodeParser{}
	state := store.SyncState{
		SourcePath: logFile,
		Tool:       "claude_code",
		LastOffset: 0,
	}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 valid assistant request, got %d", len(reqs))
	}
	if newState.LastOffset != int64(len(content)) {
		t.Fatalf("expected offset %d, got %d", len(content), newState.LastOffset)
	}
}

func TestClaudeCodeParser_BillingErrorStatus(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "error_session.jsonl")

	errorLine := `{"type":"assistant","uuid":"12074963-9a6a-484a-b018-06fa1935ca69","timestamp":"2026-09-19T19:47:49.318Z","message":{"model":"<synthetic>","role":"assistant","usage":{"input_tokens":0,"output_tokens":0,"cache_creation_input_tokens":0,"cache_read_input_tokens":0},"content":[{"type":"text","text":"Credit balance is too low"}]},"error":"billing_error","isApiErrorMessage":true}` + "\n"
	if err := os.WriteFile(logFile, []byte(errorLine), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.ClaudeCodeParser{}
	state := store.SyncState{
		SourcePath: logFile,
		Tool:       "claude_code",
		LastOffset: 0,
	}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request parsed from error event, got %d", len(reqs))
	}
	if reqs[0].Status != "error" {
		t.Errorf("expected status 'error' for billing error event, got %q", reqs[0].Status)
	}
}

func TestClaudeCodeParser_ToolCallsAndErrors(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "tools_session.jsonl")

	content := `{"type":"assistant","message":{"model":"claude-3-7-sonnet","content":[{"type":"text","text":"running command"},{"type":"tool_use","id":"toolu_1","name":"Bash","input":{"command":"ls"}}],"usage":{"input_tokens":500,"output_tokens":50,"cache_read_input_tokens":200,"cache_creation_input_tokens":0}}}` + "\n" +
		`{"type":"tool_result","tool_use_id":"toolu_1","is_error":true,"content":"permission denied"}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.ClaudeCodeParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "claude_code", LastOffset: 0}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	r := reqs[0]
	if r.ToolCallCount != 1 {
		t.Errorf("expected 1 tool call, got %d", r.ToolCallCount)
	}
	if r.ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error, got %d", r.ToolErrorCount)
	}
	if len(r.ToolCalls) != 1 {
		t.Fatalf("expected 1 ToolCall in slice, got %d", len(r.ToolCalls))
	}
	if r.ToolCalls[0].ToolName != "Bash" || r.ToolCalls[0].Status != "error" {
		t.Errorf("unexpected tool call details: %+v", r.ToolCalls[0])
	}
}

