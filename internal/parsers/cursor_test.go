package parsers_test

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	_ "github.com/mattn/go-sqlite3"
)

func createMockCursorVSCDB(t *testing.T, composerJSON string) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);`)
	if err != nil {
		t.Fatalf("failed to create table: %v", err)
	}

	if composerJSON != "" {
		_, err = db.Exec(`INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', ?);`, composerJSON)
		if err != nil {
			t.Fatalf("failed to insert composerData: %v", err)
		}
	}
	return dbPath
}

func TestCursorParser_Parse(t *testing.T) {
	mockJSON := `{
		"allComposers": [
			{
				"composerId": "comp-123",
				"conversation": [
					{ "type": "user", "text": "Fix bug" },
					{ "type": "ai", "text": "Fixed it", "modelType": "claude-3-5-sonnet" }
				]
			}
		]
	}`

	dbPath := createMockCursorVSCDB(t, mockJSON)
	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.Tool != "cursor" {
		t.Errorf("expected tool cursor, got %s", req.Tool)
	}
	if req.ID != "cursor:comp-123:0" {
		t.Errorf("expected ID cursor:comp-123:0, got %s", req.ID)
	}
	if req.SessionID != "comp-123" {
		t.Errorf("expected session comp-123, got %s", req.SessionID)
	}
	if req.PromptText != "Fix bug" {
		t.Errorf("expected prompt text 'Fix bug', got '%s'", req.PromptText)
	}
	if req.ResponseText != "Fixed it" {
		t.Errorf("expected response text 'Fixed it', got '%s'", req.ResponseText)
	}
	if req.Model != "claude-3-5-sonnet" {
		t.Errorf("expected model claude-3-5-sonnet, got %s", req.Model)
	}
	if req.Status != "estimated" {
		t.Errorf("expected status 'estimated', got %s", req.Status)
	}
	if req.InputTokens <= 0 {
		t.Errorf("expected positive input tokens, got %d", req.InputTokens)
	}
	if req.OutputTokens <= 0 {
		t.Errorf("expected positive output tokens, got %d", req.OutputTokens)
	}
	if req.TotalTokens != req.InputTokens+req.OutputTokens {
		t.Errorf("expected total tokens %d, got %d", req.InputTokens+req.OutputTokens, req.TotalTokens)
	}
	if req.CostUSD == nil || *req.CostUSD <= 0 {
		t.Errorf("expected positive cost for claude-3-5-sonnet, got %v", req.CostUSD)
	}

	// Test idempotency
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 requests on second parse, got %d", len(reqs2))
	}
}

func TestCursorParser_IncrementalTurn(t *testing.T) {
	mockJSON1 := `{
		"allComposers": [
			{
				"composerId": "comp-inc",
				"conversation": [
					{ "type": "user", "text": "Turn 1" },
					{ "type": "ai", "text": "Response 1", "modelType": "claude-3-5-sonnet" }
				]
			}
		]
	}`

	dbPath := createMockCursorVSCDB(t, mockJSON1)
	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs1, newState1, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("First parse failed: %v", err)
	}
	if len(reqs1) != 1 {
		t.Fatalf("expected 1 initial request, got %d", len(reqs1))
	}

	// Wait briefly to ensure file modification timestamp advances
	time.Sleep(50 * time.Millisecond)

	mockJSON2 := `{
		"allComposers": [
			{
				"composerId": "comp-inc",
				"conversation": [
					{ "type": "user", "text": "Turn 1" },
					{ "type": "ai", "text": "Response 1", "modelType": "claude-3-5-sonnet" },
					{ "type": "user", "text": "Turn 2" },
					{ "type": "ai", "text": "Response 2", "modelType": "claude-3-5-sonnet" }
				]
			}
		]
	}`

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`UPDATE ItemTable SET value = ? WHERE key = 'composer.composerData'`, mockJSON2)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	reqs2, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState1)
	if err != nil {
		t.Fatalf("Second parse failed: %v", err)
	}
	if len(reqs2) != 1 {
		t.Fatalf("expected exactly 1 incremental request, got %d", len(reqs2))
	}
	if reqs2[0].ID != "cursor:comp-inc:1" {
		t.Errorf("expected ID cursor:comp-inc:1, got %s", reqs2[0].ID)
	}
	if reqs2[0].PromptText != "Turn 2" {
		t.Errorf("expected prompt 'Turn 2', got '%s'", reqs2[0].PromptText)
	}
}

func TestCursorParser_AIChatData(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	chatJSON := `{
		"tabs": [
			{
				"tabId": "tab-999",
				"bubbles": [
					{ "type": "user", "text": "Chat question" },
					{ "type": "ai", "text": "Chat answer", "modelType": "gpt-4o" }
				]
			}
		]
	}`
	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('workbench.panel.aichat.chatdata', ?);
	`, chatJSON)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 request from chatdata, got %d", len(reqs))
	}
	if reqs[0].SessionID != "tab-999" {
		t.Errorf("expected session tab-999, got %s", reqs[0].SessionID)
	}
	if reqs[0].Model != "gpt-4o" {
		t.Errorf("expected model gpt-4o, got %s", reqs[0].Model)
	}
}

func TestCursorParser_NativeTokens(t *testing.T) {
	mockJSON := `{
		"allComposers": [
			{
				"composerId": "comp-native",
				"conversation": [
					{ "type": "user", "text": "Hello" },
					{
						"type": "ai",
						"text": "World",
						"modelType": "gpt-4o",
						"inputTokens": 100,
						"outputTokens": 50
					}
				]
			}
		]
	}`

	dbPath := createMockCursorVSCDB(t, mockJSON)
	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.Status != "success" {
		t.Errorf("expected status 'success' for native tokens, got %s", req.Status)
	}
	if req.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", req.InputTokens)
	}
	if req.OutputTokens != 50 {
		t.Errorf("expected 50 output tokens, got %d", req.OutputTokens)
	}
	if req.TotalTokens != 150 {
		t.Errorf("expected 150 total tokens, got %d", req.TotalTokens)
	}
}

func TestCursorParser_UnknownModelZeroCost(t *testing.T) {
	mockJSON := `{
		"allComposers": [
			{
				"composerId": "comp-unknown",
				"conversation": [
					{ "type": "user", "text": "What is cursor?" },
					{ "type": "ai", "text": "An AI code editor.", "modelType": "cursor-small" }
				]
			}
		]
	}`

	dbPath := createMockCursorVSCDB(t, mockJSON)
	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.CostUSD == nil {
		t.Fatal("expected non-nil CostUSD")
	}
	if *req.CostUSD != 0.0 {
		t.Errorf("expected $0.00 cost for unknown model cursor-small, got %f", *req.CostUSD)
	}
}

func TestCursorParser_ToolCalls(t *testing.T) {
	mockJSON := `{
		"allComposers": [
			{
				"composerId": "comp-tools",
				"conversation": [
					{ "type": "user", "text": "Run tests and edit file" },
					{
						"type": "ai",
						"text": "Done",
						"modelType": "claude-3-5-sonnet",
						"actions": [
							{ "type": "run_command", "command": "go test ./...", "status": "success" },
							{ "type": "edit_file", "filePath": "main.go", "status": "error" }
						]
					}
				]
			}
		]
	}`

	dbPath := createMockCursorVSCDB(t, mockJSON)
	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 request, got %d", len(reqs))
	}

	req := reqs[0]
	if req.ToolCallCount != 2 {
		t.Errorf("expected 2 tool calls, got %d", req.ToolCallCount)
	}
	if req.ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error, got %d", req.ToolErrorCount)
	}
	if len(req.ToolCalls) != 2 {
		t.Fatalf("expected 2 tool call objects, got %d", len(req.ToolCalls))
	}
	if req.ToolCalls[0].ToolName != "run_command" || req.ToolCalls[0].Status != "success" {
		t.Errorf("tool call 0 mismatch: %+v", req.ToolCalls[0])
	}
	if req.ToolCalls[1].ToolName != "edit_file" || req.ToolCalls[1].Status != "error" {
		t.Errorf("tool call 1 mismatch: %+v", req.ToolCalls[1])
	}
}

func TestCursorParser_Discover(t *testing.T) {
	tmpDir := t.TempDir()
	workspaceDir := filepath.Join(tmpDir, "workspaceStorage", "hash1")
	if err := os.MkdirAll(workspaceDir, 0755); err != nil {
		t.Fatal(err)
	}

	mockJSON := `{"allComposers": [{"composerId": "c1", "conversation": []}]}`
	dbPath := filepath.Join(workspaceDir, "state.vscdb")
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', '` + mockJSON + `');
	`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CursorParser{}
	sources, err := parser.Discover(config.ToolConfig{
		Paths: []string{filepath.Join(tmpDir, "workspaceStorage")},
	})
	if err != nil {
		t.Fatalf("Discover failed: %v", err)
	}
	if len(sources) != 1 {
		t.Fatalf("expected 1 discovered source, got %d", len(sources))
	}
	if sources[0].Path != dbPath {
		t.Errorf("expected %s, got %s", dbPath, sources[0].Path)
	}
	if sources[0].Type != "sqlite" {
		t.Errorf("expected sqlite type, got %s", sources[0].Type)
	}
}

func TestCursorParser_Registered(t *testing.T) {
	p, ok := parsers.Get("cursor")
	if !ok || p == nil {
		t.Fatal("expected cursor parser to be registered")
	}
	if p.Name() != "cursor" {
		t.Errorf("expected name cursor, got %s", p.Name())
	}
}

func TestCursorParser_DefensiveMissingTable(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`CREATE TABLE OtherTable (id TEXT PRIMARY KEY);`)
	db.Close()
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs, _, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err == nil {
		t.Logf("Parse returned nil error as expected or handled")
	}
	if len(reqs) != 0 {
		t.Errorf("expected 0 requests, got %d", len(reqs))
	}
}
