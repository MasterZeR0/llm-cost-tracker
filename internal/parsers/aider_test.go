package parsers_test

import (
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestAiderParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/aider_analytics.jsonl")
	parser := &parsers.AiderParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "aider"}
	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 message_send event, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "aider" {
		t.Errorf("expected aider, got %s", req.Tool)
	}
	if req.Model != "anthropic/claude-3-5-sonnet-20241022" {
		t.Errorf("expected claude-3-5-sonnet model, got %s", req.Model)
	}
	if req.InputTokens != 114112 || req.OutputTokens != 94 {
		t.Errorf("tokens mismatch: in=%d, out=%d", req.InputTokens, req.OutputTokens)
	}
	if req.TotalTokens != 114206 {
		t.Errorf("total tokens mismatch: %d", req.TotalTokens)
	}
	if req.CostUSD == nil || *req.CostUSD != 0.3423 {
		t.Errorf("expected cost 0.3423, got %v", req.CostUSD)
	}
	expectedTime := time.Unix(1742560429, 0).UTC()
	if !req.Timestamp.Equal(expectedTime) {
		t.Errorf("expected timestamp %v, got %v", expectedTime, req.Timestamp)
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

func TestAiderParser_Name(t *testing.T) {
	p := &parsers.AiderParser{}
	if p.Name() != "aider" {
		t.Errorf("expected aider, got %s", p.Name())
	}
}

func TestAiderParser_Registration(t *testing.T) {
	p, ok := parsers.Get("aider")
	if !ok || p == nil {
		t.Fatal("expected aider parser to be registered via init()")
	}
	if p.Name() != "aider" {
		t.Errorf("expected aider name, got %s", p.Name())
	}
}

func TestAiderParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "analytics.jsonl")
	if err := os.WriteFile(logFile, []byte("{}"), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AiderParser{}

	// When AnalyticsLog is configured and exists
	cfg := config.ToolConfig{
		AnalyticsLog: logFile,
	}
	sources, err := parser.Discover(cfg)
	if err != nil {
		t.Fatalf("unexpected error during Discover: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Path != logFile || sources[0].Type != "jsonl" {
		t.Errorf("unexpected source: %+v", sources[0])
	}

	// When AnalyticsLog does not exist
	cfgMissing := config.ToolConfig{
		AnalyticsLog: filepath.Join(tmpDir, "nonexistent.jsonl"),
	}
	sourcesMissing, err := parser.Discover(cfgMissing)
	if err != nil {
		t.Fatalf("unexpected error during Discover: %v", err)
	}
	if len(sourcesMissing) != 0 {
		t.Fatalf("expected 0 sources for nonexistent log, got %d", len(sourcesMissing))
	}

	// When AnalyticsLog is empty
	cfgEmpty := config.ToolConfig{}
	sourcesEmpty, err := parser.Discover(cfgEmpty)
	if err != nil {
		t.Fatalf("unexpected error during Discover: %v", err)
	}
	if len(sourcesEmpty) != 0 {
		t.Fatalf("expected 0 sources for empty config, got %d", len(sourcesEmpty))
	}
}

func TestAiderParser_TruncationAndIncompleteLine(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "analytics.jsonl")

	line1 := `{"event":"message_send","properties":{"main_model":"gpt-4o","prompt_tokens":100,"completion_tokens":50,"total_tokens":150,"cost":0.01},"time":1742560400}` + "\n"
	if err := os.WriteFile(logFile, []byte(line1), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AiderParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "aider", LastOffset: 0}
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
	_, err = f.WriteString(`{"event":"message_send","properties":{`)
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
	line2 := `{"event":"message_send","properties":{"main_model":"gpt-4o-mini","prompt_tokens":200,"completion_tokens":20,"total_tokens":220,"cost":0.002},"time":1742560500}` + "\n"
	if err := os.WriteFile(logFile, []byte(line2), 0644); err != nil {
		t.Fatal(err)
	}

	staleState := store.SyncState{SourcePath: logFile, Tool: "aider", LastOffset: 99999}
	reqsReset, resetState, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, staleState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqsReset) != 1 {
		t.Fatalf("expected 1 request after truncation reset, got %d", len(reqsReset))
	}
	if reqsReset[0].Model != "gpt-4o-mini" {
		t.Errorf("expected gpt-4o-mini, got %s", reqsReset[0].Model)
	}
	if resetState.LastOffset != int64(len(line2)) {
		t.Errorf("expected offset %d, got %d", len(line2), resetState.LastOffset)
	}
}

func TestAiderParser_NonExistentFile(t *testing.T) {
	parser := &parsers.AiderParser{}
	state := store.SyncState{SourcePath: "/path/to/nonexistent/analytics.jsonl", Tool: "aider"}
	_, _, err := parser.Parse(parsers.DataSource{Path: state.SourcePath, Type: "jsonl"}, state)
	if err == nil {
		t.Fatal("expected error for non-existent file, got nil")
	}
}

func TestAiderParser_IgnoreOtherEvents(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "analytics.jsonl")

	lines := `{"event":"other_event","properties":{}}` + "\n" +
		`{"event":"message_send","properties":{"main_model":"test-model","prompt_tokens":10,"completion_tokens":5,"total_tokens":15},"time":1742560000}` + "\n" +
		`{"event":"session_start","properties":{}}` + "\n"
	if err := os.WriteFile(logFile, []byte(lines), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AiderParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "aider"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Model != "test-model" {
		t.Errorf("expected test-model, got %s", reqs[0].Model)
	}
}
