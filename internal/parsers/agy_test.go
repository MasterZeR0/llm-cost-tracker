package parsers_test

import (
	"encoding/binary"
	"os"
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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

func TestAGYParser_SameSecondDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	// Two PLANNER_RESPONSE events at the exact same second
	content := `{"type":"PLANNER_RESPONSE","created_at":"2026-10-17T11:51:18Z","input_tokens":100,"output_tokens":50}` + "\n" +
		`{"type":"PLANNER_RESPONSE","created_at":"2026-10-17T11:51:18Z","input_tokens":200,"output_tokens":80}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "agy"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests parsed, got %d", len(reqs))
	}

	// Insert into real SQLite store to test UNIQUE constraint
	dbPath := filepath.Join(tmpDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	inserted, err := st.InsertRequests(reqs)
	if err != nil {
		t.Fatalf("unexpected error inserting requests: %v", err)
	}
	if inserted != 2 {
		t.Fatalf("expected 2 records inserted (neither dropped by same-second collision), got %d", inserted)
	}

	// Re-inserting the same requests should insert 0 (proper deduplication)
	insertedReplay, err := st.InsertRequests(reqs)
	if err != nil {
		t.Fatalf("unexpected error on replay insert: %v", err)
	}
	if insertedReplay != 0 {
		t.Fatalf("expected 0 new records on duplicate replay, got %d", insertedReplay)
	}
}

func TestAGYParser_ModelFromConversationDB(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "test-session-123"

	// Create directory structure: <tmpDir>/brain/<sessionID>/.system_generated/logs/transcript.jsonl
	brainLogDir := filepath.Join(tmpDir, "brain", sessionID, ".system_generated", "logs")
	if err := os.MkdirAll(brainLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(brainLogDir, "transcript.jsonl")

	transcriptContent := `{"step_index":0,"type":"USER_INPUT","content":"hello"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-10-18T00:00:01Z","input_tokens":100,"output_tokens":50}` + "\n" +
		`{"step_index":2,"type":"PLANNER_RESPONSE","created_at":"2026-10-18T00:00:05Z","input_tokens":200,"output_tokens":80}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(transcriptContent), 0644); err != nil {
		t.Fatal(err)
	}

	// Create conversation DB: <tmpDir>/conversations/<sessionID>.db
	convDir := filepath.Join(tmpDir, "conversations")
	if err := os.MkdirAll(convDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(convDir, sessionID+".db")
	db, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB().Exec(`CREATE TABLE gen_metadata (idx integer primary key, data blob)`)
	if err != nil {
		t.Fatal(err)
	}

	// Turn 0: gemini-3.8-flash-n -> should normalize to gemini-3-8-flash
	m0 := "gemini-3.8-flash-n"
	blob0 := append([]byte{0x12, 0x02, 0x05, 0x06, 0x9a, 0x01, byte(len(m0))}, []byte(m0)...)
	// Turn 1: claude-opus-4-6-thinking -> should normalize to claude-opus-4-6-thinking
	m1 := "claude-opus-4-6-thinking"
	blob1 := append([]byte{0x12, 0x02, 0x05, 0x06, 0x9a, 0x01, byte(len(m1))}, []byte(m1)...)

	_, err = db.DB().Exec("INSERT INTO gen_metadata (idx, data) VALUES (?, ?), (?, ?)", 0, blob0, 1, blob1)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: transcriptPath, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: transcriptPath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 2 {
		t.Fatalf("expected 2 requests, got %d", len(reqs))
	}

	if reqs[0].Model != "gemini-3-8-flash" {
		t.Errorf("turn 0 expected model gemini-3-8-flash, got %s", reqs[0].Model)
	}
	if reqs[1].Model != "claude-opus-4-6-thinking" {
		t.Errorf("turn 1 expected model claude-opus-4-6-thinking, got %s", reqs[1].Model)
	}
}

func TestAGYParser_ModelFromSettingsChange(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	content := `{"step_index":0,"type":"USER_INPUT","content":"<USER_SETTINGS_CHANGE>\nThe user changed setting ` + "`Model Selection`" + ` from None to Gemini 3.8 Flash (Medium).\n</USER_SETTINGS_CHANGE>"}` + "\n" +
		`{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-10-18T00:00:01Z","input_tokens":100,"output_tokens":50}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: logFile, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Model != "gemini-3-8-flash" {
		t.Errorf("expected model gemini-3-8-flash from settings change, got %s", reqs[0].Model)
	}
}

func TestAGYParser_ModelFromSettingsJSON(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "test-session-settings"

	// Create settings.json
	settingsPath := filepath.Join(tmpDir, "settings.json")
	if err := os.WriteFile(settingsPath, []byte(`{"model":"Gemini 3.8 Flash (Medium)"}`), 0644); err != nil {
		t.Fatal(err)
	}

	brainLogDir := filepath.Join(tmpDir, "brain", sessionID, ".system_generated", "logs")
	if err := os.MkdirAll(brainLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(brainLogDir, "transcript.jsonl")

	content := `{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-10-18T00:00:01Z","input_tokens":100,"output_tokens":50}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: transcriptPath, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: transcriptPath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Model != "gemini-3-8-flash" {
		t.Errorf("expected model gemini-3-8-flash from settings.json, got %s", reqs[0].Model)
	}
}

func TestAGYParser_TokenBackfillFromStepsDB(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "test-session-tokens-backfill"

	brainLogDir := filepath.Join(tmpDir, "brain", sessionID, ".system_generated", "logs")
	if err := os.MkdirAll(brainLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(brainLogDir, "transcript.jsonl")

	// Transcript with 0/missing tokens (older AGY format)
	content := `{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-09-18T10:00:00Z"}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	convDir := filepath.Join(tmpDir, "conversations")
	if err := os.MkdirAll(convDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(convDir, sessionID+".db")
	db, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB().Exec(`
		CREATE TABLE gen_metadata (idx integer primary key, data blob);
		CREATE TABLE steps (idx integer primary key, metadata blob);
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Model: gemini-pro-default -> gemini-2-pro
	rawModel := "gemini-pro-default"
	blobGen := append([]byte{0x12, 0x02, 0x05, 0x06, 0x9a, 0x01, byte(len(rawModel))}, []byte(rawModel)...)
	_, _ = db.DB().Exec("INSERT INTO gen_metadata (idx, data) VALUES (0, ?)", blobGen)

	// Step 1 token submessage:
	// field 1 = 1016 (0x08, 0xf8, 0x07)
	// field 2 = 14495 (0x10, 0x9f, 0x71)
	// field 3 = 494 (0x18, 0xee, 0x03)
	// field 5 = 1200 (0x28, 0xb0, 0x09)
	sub := []byte{0x08, 0xf8, 0x07, 0x10, 0x9f, 0x71, 0x18, 0xee, 0x03, 0x28, 0xb0, 0x09}
	stepMeta := append([]byte{0x0a, 0x05, 0x01, 0x02, 0x03, 0x04, 0x05, 0x4a, byte(len(sub))}, sub...)
	_, err = db.DB().Exec("INSERT INTO steps (idx, metadata) VALUES (1, ?)", stepMeta)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: transcriptPath, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: transcriptPath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.Model != "gemini-2-pro" {
		t.Errorf("expected model gemini-2-pro, got %s", r.Model)
	}
	if r.InputTokens != 14495 {
		t.Errorf("expected InputTokens 14495, got %d", r.InputTokens)
	}
	if r.OutputTokens != 494 {
		t.Errorf("expected OutputTokens 494, got %d", r.OutputTokens)
	}
	if r.CacheReadTokens != 1200 {
		t.Errorf("expected CacheReadTokens 1200, got %d", r.CacheReadTokens)
	}
	if r.TotalTokens != 14495+494 {
		t.Errorf("expected TotalTokens 14989, got %d", r.TotalTokens)
	}
}

func TestAGYParser_ToolCallsAndErrors(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	content := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"list files"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","created_at":"2026-10-18T10:00:00Z","input_tokens":1000,"output_tokens":150,"cache_read_tokens":4000,"tool_calls":[{"name":"run_command","args":{"CommandLine":"ls -la"}},{"name":"invoke_subagent","args":{"Role":"Researcher"}}]}` + "\n" +
		`{"step_index":2,"source":"SYSTEM","type":"RUN_COMMAND","status":"ERROR","created_at":"2026-10-18T10:00:02Z","exit_code":1}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.AGYParser{}
	reqs, _, err := p.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, store.SyncState{SourcePath: logFile, Tool: "agy"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	r := reqs[0]
	if r.StepIndex != 1 {
		t.Errorf("expected step_index 1, got %d", r.StepIndex)
	}
	if r.CacheReadTokens != 4000 || r.InputTokens != 1000 {
		t.Errorf("expected 4000 hit, 1000 miss, got %d hit, %d miss", r.CacheReadTokens, r.InputTokens)
	}
	if r.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", r.ToolCallCount)
	}
	if r.SubagentCallCount != 1 {
		t.Errorf("expected 1 subagent call, got %d", r.SubagentCallCount)
	}
	if r.ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error from RUN_COMMAND failure, got %d", r.ToolErrorCount)
	}
	if len(r.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool call models, got %d", len(r.ToolCalls))
	}
	if r.ToolCalls[0].ToolName != "run_command" || r.ToolCalls[0].Status != "error" {
		t.Errorf("expected run_command to be error, got %+v", r.ToolCalls[0])
	}
}

func TestAGYParser_LatencyFromStepsDB(t *testing.T) {
	tmpDir := t.TempDir()
	sessionID := "test-session-latency-db"

	brainLogDir := filepath.Join(tmpDir, "brain", sessionID, ".system_generated", "logs")
	if err := os.MkdirAll(brainLogDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(brainLogDir, "transcript.jsonl")

	content := `{"step_index":1,"type":"PLANNER_RESPONSE","created_at":"2026-10-18T10:00:00Z","input_tokens":100,"output_tokens":50}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	convDir := filepath.Join(tmpDir, "conversations")
	if err := os.MkdirAll(convDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(convDir, sessionID+".db")
	db, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.DB().Exec(`CREATE TABLE steps (idx integer primary key, metadata blob);`)
	if err != nil {
		t.Fatal(err)
	}

	// Helper to build protobuf timestamp field
	buildTimestampField := func(fnum int, sec int64, nanos int64) []byte {
		var sub []byte
		sub = append(sub, 0x08) // field 1 varint (seconds)
		var bSec [10]byte
		nSec := binary.PutUvarint(bSec[:], uint64(sec))
		sub = append(sub, bSec[:nSec]...)

		if nanos > 0 {
			sub = append(sub, 0x10) // field 2 varint (nanos)
			var bNanos [10]byte
			nNanos := binary.PutUvarint(bNanos[:], uint64(nanos))
			sub = append(sub, bNanos[:nNanos]...)
		}

		var res []byte
		var bTag [10]byte
		nTag := binary.PutUvarint(bTag[:], uint64((fnum<<3)|2))
		res = append(res, bTag[:nTag]...)

		var bLen [10]byte
		nLen := binary.PutUvarint(bLen[:], uint64(len(sub)))
		res = append(res, bLen[:nLen]...)
		res = append(res, sub...)
		return res
	}

	// Start at 1700000000s, end at 1700000004s + 500ms -> duration = 4500ms
	f1 := buildTimestampField(1, 1700000000, 0)
	f7 := buildTimestampField(7, 1700000004, 500_000_000)

	var stepMeta []byte
	stepMeta = append(stepMeta, f1...)
	stepMeta = append(stepMeta, f7...)

	_, err = db.DB().Exec("INSERT INTO steps (idx, metadata) VALUES (1, ?)", stepMeta)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.AGYParser{}
	state := store.SyncState{SourcePath: transcriptPath, Tool: "agy"}
	reqs, _, err := parser.Parse(parsers.DataSource{Path: transcriptPath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.LatencyMS == nil {
		t.Fatal("expected LatencyMS to be populated from steps.metadata, got nil")
	}
	if *r.LatencyMS != 4500 {
		t.Errorf("expected LatencyMS 4500, got %d", *r.LatencyMS)
	}
}

func TestAGYParser_LatencyFallbackFromTranscript(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	// 5 seconds between user input and planner response
	content := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","created_at":"2026-10-18T10:00:00Z","content":"hello"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","created_at":"2026-10-18T10:00:05Z","input_tokens":100,"output_tokens":50}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &parsers.AGYParser{}
	reqs, _, err := p.Parse(parsers.DataSource{Path: logFile, Type: "jsonl"}, store.SyncState{SourcePath: logFile, Tool: "agy"})
	if err != nil {
		t.Fatalf("unexpected parse error: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	r := reqs[0]
	if r.LatencyMS == nil {
		t.Fatal("expected fallback LatencyMS, got nil")
	}
	if *r.LatencyMS != 5000 {
		t.Errorf("expected LatencyMS 5000, got %d", *r.LatencyMS)
	}
}



