package parsers_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestCodexParser_Parse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state_5.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			model_provider TEXT,
			tokens_used INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		);
		INSERT INTO threads (id, model_provider, tokens_used, created_at, updated_at)
		VALUES ('thread-1', 'openai/gpt-4o', 4500, '2026-10-17 10:00:00', '2026-10-17 10:05:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 thread request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "codex" {
		t.Errorf("expected codex, got %s", req.Tool)
	}
	if req.TotalTokens != 4500 {
		t.Errorf("expected 4500 tokens, got %d", req.TotalTokens)
	}
	if req.InputTokens != 2250 || req.OutputTokens != 2250 {
		t.Errorf("expected 2250 input/output tokens, got %d / %d", req.InputTokens, req.OutputTokens)
	}
	if req.Model != "openai/gpt-4o" {
		t.Errorf("expected openai/gpt-4o model, got %s", req.Model)
	}
	if req.SessionID != "thread-1" {
		t.Errorf("expected thread-1 session id, got %s", req.SessionID)
	}
	if newState.LastRowID != "thread-1" {
		t.Errorf("expected LastRowID to update, got %s", newState.LastRowID)
	}
	if newState.LastModified == nil {
		t.Errorf("expected LastModified to be updated")
	}

	// Incremental sync test: no new rows
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on incremental sync, got %d", len(reqs2))
	}

	// Add another row with later timestamp
	_, err = db.Exec(`
		INSERT INTO threads (id, model_provider, tokens_used, created_at, updated_at)
		VALUES ('thread-2', 'openai/o3-mini', 1200, '2026-10-17 11:00:00', '2026-10-17 11:05:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	reqs3, newState3, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs3) != 1 {
		t.Fatalf("expected 1 new request on incremental sync, got %d", len(reqs3))
	}
	if reqs3[0].SessionID != "thread-2" {
		t.Errorf("expected session id thread-2, got %s", reqs3[0].SessionID)
	}
	if newState3.LastRowID != "thread-2" {
		t.Errorf("expected LastRowID thread-2, got %s", newState3.LastRowID)
	}
}

func TestCodexParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state_5.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.CodexParser{}
	sources, err := parser.Discover(config.ToolConfig{
		Paths: []string{dbPath, filepath.Join(tmpDir, "nonexistent.sqlite")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 source, got %d", len(sources))
	}
	if sources[0].Path != dbPath {
		t.Errorf("expected %s, got %s", dbPath, sources[0].Path)
	}
	if sources[0].Type != "sqlite" {
		t.Errorf("expected sqlite, got %s", sources[0].Type)
	}
}

func TestCodexParser_Registered(t *testing.T) {
	p, ok := parsers.Get("codex")
	if !ok || p == nil {
		t.Fatal("expected codex parser to be registered")
	}
	if p.Name() != "codex" {
		t.Errorf("expected name codex, got %s", p.Name())
	}
}

func TestCodexParser_DefensiveMissingTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE dummy (id TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("expected no error for missing threads table, got %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

func TestCodexParser_DefaultsAndNulls(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "nulls.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			model_provider TEXT,
			tokens_used INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		);
		INSERT INTO threads (id, model_provider, tokens_used, created_at, updated_at)
		VALUES ('thread-null', NULL, NULL, NULL, NULL);
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	if reqs[0].Model != "gpt-4o" {
		t.Errorf("expected default model gpt-4o, got %s", reqs[0].Model)
	}
	if reqs[0].TotalTokens != 0 {
		t.Errorf("expected 0 total tokens for null, got %d", reqs[0].TotalTokens)
	}
	if newState.LastRowID != "thread-null" {
		t.Errorf("expected LastRowID thread-null, got %s", newState.LastRowID)
	}
}

func TestCodexParser_IntegerEpochSeconds(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state_5.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			model_provider TEXT,
			tokens_used INTEGER,
			created_at INTEGER NOT NULL
		);
		INSERT INTO threads (id, model_provider, tokens_used, created_at)
		VALUES ('thread-epoch-1', 'openai/gpt-4o', 1000, 1713000000);
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.SessionID != "thread-epoch-1" {
		t.Errorf("expected thread-epoch-1, got %s", req.SessionID)
	}
	if req.TotalTokens != 1000 {
		t.Errorf("expected 1000 tokens, got %d", req.TotalTokens)
	}
	// 1713000000 -> 2024-04-13 09:20:00 UTC
	expectedTime := time.Unix(1713000000, 0).UTC()
	if req.Timestamp.Unix() != expectedTime.Unix() {
		t.Errorf("expected timestamp near %v, got %v", expectedTime, req.Timestamp)
	}

	// Incremental sync
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 requests on incremental sync, got %d", len(reqs2))
	}

	// Insert second row with later epoch seconds
	_, err = db.Exec(`
		INSERT INTO threads (id, model_provider, tokens_used, created_at)
		VALUES ('thread-epoch-2', 'openai/o3-mini', 2000, 1713001000);
	`)
	if err != nil {
		t.Fatal(err)
	}

	reqs3, newState3, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs3) != 1 {
		t.Fatalf("expected 1 new request on incremental sync, got %d", len(reqs3))
	}
	if reqs3[0].SessionID != "thread-epoch-2" {
		t.Errorf("expected thread-epoch-2, got %s", reqs3[0].SessionID)
	}
	if newState3.LastRowID != "thread-epoch-2" {
		t.Errorf("expected LastRowID thread-epoch-2, got %s", newState3.LastRowID)
	}
}

func TestCodexParser_IntegerEpochMilliseconds(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state_5.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			model_provider TEXT,
			tokens_used INTEGER,
			created_at INTEGER NOT NULL
		);
		INSERT INTO threads (id, model_provider, tokens_used, created_at)
		VALUES ('thread-milli-1', 'openai/gpt-4o', 500, 1713000000123);
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.SessionID != "thread-milli-1" {
		t.Errorf("expected thread-milli-1, got %s", req.SessionID)
	}
	expectedTime := time.UnixMilli(1713000000123).UTC()
	if req.Timestamp.UnixMilli() < expectedTime.UnixMilli() || req.Timestamp.UnixMilli() > expectedTime.UnixMilli()+100 {
		t.Errorf("expected timestamp near %v, got %v", expectedTime, req.Timestamp)
	}

	// Incremental sync
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 requests on incremental sync, got %d", len(reqs2))
	}
}
