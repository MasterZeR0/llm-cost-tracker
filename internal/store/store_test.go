package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestStore_InitAndDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nested", "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC().Truncate(time.Second)
	req1 := store.Request{
		ID:           "req-1",
		Tool:         "claude_code",
		SessionID:    "sess-1",
		Timestamp:    now,
		Model:        "claude-opus-5",
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		CostUSD:      pointerFloat(0.02),
		LatencyMS:    pointerInt(1200),
		Status:       "success",
	}

	inserted, err := st.InsertRequests([]store.Request{req1})
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected 1 inserted, got %d", inserted)
	}

	// Re-insert exact same request (dedup test by primary key)
	inserted2, err := st.InsertRequests([]store.Request{req1})
	if err != nil {
		t.Fatalf("failed to re-insert: %v", err)
	}
	if inserted2 != 0 {
		t.Fatalf("expected 0 inserted on duplicate, got %d", inserted2)
	}

	// Re-insert with different ID but same (tool, session_id, timestamp, model) unique tuple
	reqDuplicateTuple := req1
	reqDuplicateTuple.ID = "req-different-id"
	inserted3, err := st.InsertRequests([]store.Request{reqDuplicateTuple})
	if err != nil {
		t.Fatalf("failed to test duplicate tuple: %v", err)
	}
	if inserted3 != 0 {
		t.Fatalf("expected 0 inserted on duplicate tuple, got %d", inserted3)
	}

	// Empty slice
	insertedEmpty, err := st.InsertRequests([]store.Request{})
	if err != nil {
		t.Fatalf("failed to insert empty slice: %v", err)
	}
	if insertedEmpty != 0 {
		t.Fatalf("expected 0 for empty slice, got %d", insertedEmpty)
	}
}

func TestStore_SyncState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	state, err := st.GetSyncState("/path/to/log.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if state.LastOffset != 0 {
		t.Fatalf("expected 0 offset for new state, got %d", state.LastOffset)
	}

	now := time.Now().UTC().Truncate(time.Second)
	state.LastOffset = 1024
	state.Tool = "claude_code"
	state.LastModified = &now
	state.LastRowID = "row-42"
	if err := st.SetSyncState(*state); err != nil {
		t.Fatal(err)
	}

	retrieved, err := st.GetSyncState("/path/to/log.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if retrieved.LastOffset != 1024 {
		t.Fatalf("expected 1024 offset, got %d", retrieved.LastOffset)
	}
	if retrieved.Tool != "claude_code" {
		t.Fatalf("expected tool claude_code, got %s", retrieved.Tool)
	}
	if retrieved.LastRowID != "row-42" {
		t.Fatalf("expected row-42, got %s", retrieved.LastRowID)
	}
	if retrieved.LastModified == nil || !retrieved.LastModified.Equal(now) {
		t.Fatalf("expected last modified %v, got %v", now, retrieved.LastModified)
	}
}

func pointerFloat(v float64) *float64 { return &v }
func pointerInt(v int64) *int64       { return &v }

func TestStore_MigrationAndToolCalls(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test_migration.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	// Verify columns exist on requests table
	var colCount int
	err = st.DB().QueryRow(`SELECT count(*) FROM pragma_table_info('requests') WHERE name IN ('step_index', 'tool_call_count', 'tool_error_count', 'subagent_call_count')`).Scan(&colCount)
	if err != nil {
		t.Fatalf("failed to query pragma_table_info: %v", err)
	}
	if colCount != 4 {
		t.Fatalf("expected 4 new columns on requests table, got %d", colCount)
	}

	// Verify tool_calls table exists
	var tableExists int
	err = st.DB().QueryRow(`SELECT count(*) FROM sqlite_master WHERE type='table' AND name='tool_calls'`).Scan(&tableExists)
	if err != nil {
		t.Fatalf("failed to query sqlite_master: %v", err)
	}
	if tableExists != 1 {
		t.Fatalf("expected tool_calls table to exist, got %d", tableExists)
	}

	// Insert a request with tool calls
	reqID := "req-test-1"
	req := store.Request{
		ID:                reqID,
		Tool:              "agy",
		SessionID:         "sess-123",
		Timestamp:         time.Now().UTC(),
		Model:             "gemini-2-pro",
		InputTokens:       1000,
		OutputTokens:      200,
		CacheReadTokens:   5000,
		TotalTokens:       6200,
		StepIndex:         1,
		ToolCallCount:     2,
		ToolErrorCount:    1,
		SubagentCallCount: 0,
		ToolCalls: []store.ToolCall{
			{
				ID:        "tc-1",
				RequestID: reqID,
				SessionID: "sess-123",
				ToolName:  "run_command",
				Status:    "error",
				CreatedAt: time.Now().UTC(),
			},
			{
				ID:        "tc-2",
				RequestID: reqID,
				SessionID: "sess-123",
				ToolName:  "view_file",
				Status:    "success",
				CreatedAt: time.Now().UTC().Add(time.Second),
			},
		},
	}

	if err := st.InsertRequestsWithTools([]store.Request{req}); err != nil {
		t.Fatalf("failed to insert request with tools: %v", err)
	}

	var savedToolCount int
	err = st.DB().QueryRow(`SELECT count(*) FROM tool_calls WHERE request_id = ?`, reqID).Scan(&savedToolCount)
	if err != nil {
		t.Fatalf("failed to count tool calls: %v", err)
	}
	if savedToolCount != 2 {
		t.Fatalf("expected 2 tool calls saved, got %d", savedToolCount)
	}
}

