package parsers_test

import (
	"database/sql"
	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	"path/filepath"
	"testing"
)

func TestOpenCodeParser_Parse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "opencode.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE messages (
			id TEXT PRIMARY KEY,
			session_id TEXT,
			model TEXT,
			tokens_in INTEGER,
			tokens_out INTEGER,
			created_at DATETIME
		);
		INSERT INTO messages (id, session_id, model, tokens_in, tokens_out, created_at)
		VALUES ('msg-1', 'sess-100', 'anthropic/claude-3-5-sonnet', 1200, 350, '2026-10-17 10:00:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.OpenCodeParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "opencode"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 message request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "opencode" {
		t.Errorf("expected opencode, got %s", req.Tool)
	}
	if req.SessionID != "sess-100" {
		t.Errorf("expected sess-100, got %s", req.SessionID)
	}
	if req.Model != "anthropic/claude-3-5-sonnet" {
		t.Errorf("expected anthropic/claude-3-5-sonnet, got %s", req.Model)
	}
	if req.InputTokens != 1200 {
		t.Errorf("expected 1200 input tokens, got %d", req.InputTokens)
	}
	if req.OutputTokens != 350 {
		t.Errorf("expected 350 output tokens, got %d", req.OutputTokens)
	}
	if req.TotalTokens != 1550 {
		t.Errorf("expected 1550 total tokens, got %d", req.TotalTokens)
	}
	if req.Status != "success" {
		t.Errorf("expected success status, got %s", req.Status)
	}

	// Incremental sync
	_, err = db.Exec(`
		INSERT INTO messages (id, session_id, model, tokens_in, tokens_out, created_at)
		VALUES ('msg-2', 'sess-100', 'anthropic/claude-3-5-sonnet', 800, 200, '2026-10-17 10:15:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 1 {
		t.Fatalf("expected 1 incremental request, got %d", len(reqs2))
	}
	if reqs2[0].InputTokens != 800 {
		t.Errorf("expected 800 input tokens, got %d", reqs2[0].InputTokens)
	}
}

func TestOpenCodeParser_DefensiveMissingTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "empty.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE other_table (id TEXT PRIMARY KEY);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.OpenCodeParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "opencode"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("expected no error for missing messages table, got %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

func TestOpenCodeParser_DefensiveCorruptSchema(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "corrupt.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	// messages table exists, but doesn't have the expected columns
	_, err = db.Exec(`CREATE TABLE messages (dummy_column TEXT);`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.OpenCodeParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "opencode"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("expected no error for incompatible schema, got %v", err)
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}

func TestOpenCodeParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "opencode.db")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	if err := db.Ping(); err != nil {
		t.Fatal(err)
	}
	db.Close()

	parser := &parsers.OpenCodeParser{}
	sources, err := parser.Discover(config.ToolConfig{
		Paths: []string{dbPath, filepath.Join(tmpDir, "nonexistent.db")},
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

func TestOpenCodeParser_Registered(t *testing.T) {
	p, ok := parsers.Get("opencode")
	if !ok || p == nil {
		t.Fatal("expected opencode parser to be registered")
	}
	if p.Name() != "opencode" {
		t.Errorf("expected name opencode, got %s", p.Name())
	}
}
