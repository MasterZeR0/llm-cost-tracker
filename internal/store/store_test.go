package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
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
