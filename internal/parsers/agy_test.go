package parsers_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestAGYParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/agy_sample.jsonl")
	parser := &parsers.AGYParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "agy"}
	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 PLANNER_RESPONSE turn, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "agy" {
		t.Errorf("expected agy, got %s", req.Tool)
	}
	if req.Model != "gemini-2-pro" {
		t.Errorf("expected gemini-2-pro, got %s", req.Model)
	}
	if req.InputTokens != 15420 || req.OutputTokens != 850 {
		t.Errorf("token count mismatch: in=%d, out=%d", req.InputTokens, req.OutputTokens)
	}
	if req.CacheReadTokens != 1200 {
		t.Errorf("cache read tokens mismatch: %d", req.CacheReadTokens)
	}
	if req.TotalTokens != 15420+850 {
		t.Errorf("total tokens mismatch: %d", req.TotalTokens)
	}
	if newState.LastOffset <= 0 {
		t.Errorf("offset did not advance: %d", newState.LastOffset)
	}

	// Incremental test
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Fatalf("expected 0 new requests from end of file, got %d", len(reqs2))
	}
}

func TestAGYParser_Name(t *testing.T) {
	p := &parsers.AGYParser{}
	if p.Name() != "agy" {
		t.Errorf("expected agy, got %s", p.Name())
	}
}

func TestAGYParser_Registration(t *testing.T) {
	p, ok := parsers.Get("agy")
	if !ok || p == nil {
		t.Fatal("expected agy parser to be registered via init()")
	}
	if p.Name() != "agy" {
		t.Errorf("expected agy name, got %s", p.Name())
	}
}

func TestAGYParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	brainDir := filepath.Join(tmpDir, "brain", "session-123", ".system_generated", "logs")
	if err := os.MkdirAll(brainDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(brainDir, "transcript.jsonl")
	otherPath := filepath.Join(brainDir, "other.jsonl")

	if err := os.WriteFile(transcriptPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(otherPath, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	cfg := config.ToolConfig{
		Paths: []string{tmpDir},
	}

	sources, err := parser.Discover(cfg)
	if err != nil {
		t.Fatalf("unexpected error during Discover: %v", err)
	}

	if len(sources) != 1 {
		t.Fatalf("expected 1 transcript source, got %d", len(sources))
	}
	if sources[0].Path != transcriptPath {
		t.Errorf("expected path %s, got %s", transcriptPath, sources[0].Path)
	}
}

func TestAGYParser_SessionIDExtraction(t *testing.T) {
	tmpDir := t.TempDir()
	sessionDir := filepath.Join(tmpDir, "brain", "my-test-session-id", ".system_generated", "logs")
	if err := os.MkdirAll(sessionDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(sessionDir, "transcript.jsonl")
	line := `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:05Z","input_tokens":100,"output_tokens":50}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: transcriptPath, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: transcriptPath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].SessionID != "my-test-session-id" {
		t.Errorf("expected session ID 'my-test-session-id', got '%s'", reqs[0].SessionID)
	}
}

func TestAGYParser_TruncationAndIncompleteLine(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	line1 := `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:05Z","input_tokens":100,"output_tokens":50}` + "\n"
	if err := os.WriteFile(logFile, []byte(line1), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "agy", LastOffset: 0}
	reqs, newState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	// Incomplete line
	f, err := os.OpenFile(logFile, os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		t.Fatal(err)
	}
	_, err = f.WriteString(`{"step_index":2,"type":"PLANNER_RESPONSE"`)
	f.Close()
	if err != nil {
		t.Fatal(err)
	}

	reqs, afterIncomplete, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 0 {
		t.Fatalf("expected 0 requests from incomplete line, got %d", len(reqs))
	}
	if afterIncomplete.LastOffset != newState.LastOffset {
		t.Errorf("expected offset unchanged %d, got %d", newState.LastOffset, afterIncomplete.LastOffset)
	}

	// Truncation
	line2 := `{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:05Z","input_tokens":200,"output_tokens":60}` + "\n"
	if err := os.WriteFile(logFile, []byte(line2), 0644); err != nil {
		t.Fatal(err)
	}

	staleState := store.SyncState{SourcePath: logFile, Tool: "agy", LastOffset: 99999}
	reqsReset, resetState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, staleState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqsReset) != 1 {
		t.Fatalf("expected 1 request after truncation reset, got %d", len(reqsReset))
	}
	if reqsReset[0].InputTokens != 200 {
		t.Errorf("expected input tokens 200, got %d", reqsReset[0].InputTokens)
	}
	if resetState.LastOffset != int64(len(line2)) {
		t.Errorf("expected offset %d, got %d", len(line2), resetState.LastOffset)
	}
}

func TestAGYParser_NonExistentFile(t *testing.T) {
	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: "/path/to/nonexistent/transcript.jsonl", Tool: "agy"}
	_, _, err := parser.Parse(parsers.DataSource{Path: state.SourcePath, Type: "jsonl"}, state)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}
