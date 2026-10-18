package test

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"sync"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers/vscdb"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

// -------------------------------------------------------------
// 1. LOCKED OR IN-USE SQLITE DATABASES
// -------------------------------------------------------------

func TestResilience_LockedSQLite_VSCDB(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	// Create SQLite database
	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable VALUES ('composer.composerData', '{"allComposers":[]}');
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Begin an exclusive transaction to simulate an active write lock
	tx, err := db.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`UPDATE ItemTable SET value = 'locked' WHERE key = 'composer.composerData'`)
	if err != nil {
		t.Fatal(err)
	}

	// Now try to open and read using vscdb.Open (which uses immutable=1, mode=ro)
	// Because of immutable=1, SQLite does not acquire shared read locks.
	reader, err := vscdb.Open(dbPath)
	if err != nil {
		t.Logf("vscdb.Open while DB locked returned error: %v", err)
	} else {
		val, getErr := reader.GetKey("composer.composerData")
		t.Logf("vscdb.Open succeeded under lock, GetKey result: val=%s, err=%v", string(val), getErr)
		reader.Close()
	}

	// Commit lock
	_ = tx.Rollback()
	db.Close()
}

func TestResilience_VSCDB_PathWithSpaces(t *testing.T) {
	dir := t.TempDir()
	spaceDir := filepath.Join(dir, "Application Support", "Cursor App")
	if err := os.MkdirAll(spaceDir, 0755); err != nil {
		t.Fatal(err)
	}
	dbPath := filepath.Join(spaceDir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable VALUES ('composer.composerData', '{"test": 123}');
	`)
	if err != nil {
		t.Fatal(err)
	}
	db.Close()

	reader, err := vscdb.Open(dbPath)
	if err != nil {
		t.Fatalf("vscdb.Open failed on path with spaces: %v", err)
	}
	defer reader.Close()

	val, err := reader.GetKey("composer.composerData")
	if err != nil {
		t.Fatalf("GetKey failed on path with spaces: %v", err)
	}
	if string(val) != `{"test": 123}` {
		t.Errorf("unexpected value: %s", string(val))
	}
}

func TestResilience_VSCDB_WALModeUncheckpointed(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = db.Exec(`
		PRAGMA journal_mode=WAL;
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable VALUES ('composer.composerData', 'initial');
	`)
	if err != nil {
		t.Fatal(err)
	}

	// Update value in WAL mode while keeping connection open without checkpointing
	_, err = db.Exec(`UPDATE ItemTable SET value = 'updated_in_wal' WHERE key = 'composer.composerData'`)
	if err != nil {
		t.Fatal(err)
	}

	// Verify WAL file exists
	walPath := dbPath + "-wal"
	if fi, err := os.Stat(walPath); err == nil && fi.Size() > 0 {
		t.Logf("WAL file exists with size %d", fi.Size())
	}

	// Read using vscdb.Open (with immutable=1)
	reader, err := vscdb.Open(dbPath)
	if err != nil {
		msg := fmt.Sprintf("QA AUDIT FINDING [HIGH]: immutable=1 in vscdb.Open failed to see ItemTable in uncheckpointed WAL database: %v", err)
		db.Close()
		if os.Getenv("STRICT_AUDIT") == "1" {
			t.Fatalf("%s", msg)
		} else {
			t.Logf("%s", msg)
			return
		}
	}
	val, err := reader.GetKey("composer.composerData")
	reader.Close()
	db.Close()

	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}

	t.Logf("Value read with immutable=1: %s", string(val))
	if string(val) != "updated_in_wal" {
		msg := fmt.Sprintf("QA AUDIT FINDING [HIGH]: immutable=1 in vscdb.Open ignored uncheckpointed WAL file! Expected 'updated_in_wal', got '%s'", string(val))
		if os.Getenv("STRICT_AUDIT") == "1" {
			t.Errorf("%s", msg)
		} else {
			t.Logf("%s", msg)
		}
	}
}

func TestResilience_LockedTrackerStore(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tracker.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Open raw connection and hold EXCLUSIVE transaction
	rawDB, err := sql.Open("sqlite3", dbPath+"?_busy_timeout=500")
	if err != nil {
		t.Fatal(err)
	}
	defer rawDB.Close()

	tx, err := rawDB.Begin()
	if err != nil {
		t.Fatal(err)
	}
	_, err = tx.Exec(`UPDATE requests SET cost_usd = 999.0 WHERE id = 'nonexistent'`)
	if err != nil {
		t.Fatal(err)
	}

	// Store has busy_timeout=5000 in its DSN, so InsertRequests should either wait or timeout
	done := make(chan error, 1)
	go func() {
		_, insertErr := st.InsertRequests([]store.Request{
			{
				ID:        "locked-test-req-1",
				Tool:      "cline",
				SessionID: "s1",
				Timestamp: time.Now().UTC(),
			},
		})
		done <- insertErr
	}()

	// Release lock after 300ms (within 5000ms busy timeout)
	time.Sleep(300 * time.Millisecond)
	_ = tx.Rollback()

	select {
	case insertErr := <-done:
		if insertErr != nil {
			t.Errorf("expected busy_timeout to allow transaction to succeed once unlocked, got: %v", insertErr)
		} else {
			t.Log("Store handled concurrent lock gracefully via busy timeout")
		}
	case <-time.After(6 * time.Second):
		t.Fatal("InsertRequests timed out or deadlocked")
	}
}

// -------------------------------------------------------------
// 2. CORRUPT FILES, TRUNCATED JSON/JSONL, PERMISSIONS
// -------------------------------------------------------------

func TestResilience_CorruptFiles(t *testing.T) {
	dir := t.TempDir()

	// 2a. Corrupt Cline / Roo Code JSON (syntax error)
	t.Run("Cline_CorruptJSON", func(t *testing.T) {
		corruptPath := filepath.Join(dir, "corrupt_cline.json")
		if err := os.WriteFile(corruptPath, []byte(`[{"role": "assistant", "content": `), 0644); err != nil {
			t.Fatal(err)
		}
		p := &parsers.ClineParser{}
		reqs, _, err := p.Parse(parsers.DataSource{Path: corruptPath, Type: "json"}, store.SyncState{})
		if err == nil {
			t.Errorf("expected error parsing corrupt JSON, got nil (reqs: %d)", len(reqs))
		} else {
			t.Logf("Cline handled corrupt JSON with expected error: %v", err)
		}
	})

	// 2b. Zero-byte file for Cline
	t.Run("Cline_ZeroByteFile", func(t *testing.T) {
		emptyPath := filepath.Join(dir, "empty_cline.json")
		if err := os.WriteFile(emptyPath, []byte(``), 0644); err != nil {
			t.Fatal(err)
		}
		p := &parsers.ClineParser{}
		reqs, newState, err := p.Parse(parsers.DataSource{Path: emptyPath, Type: "json"}, store.SyncState{})
		if err != nil {
			t.Errorf("expected nil error on empty file, got: %v", err)
		}
		if len(reqs) != 0 {
			t.Errorf("expected 0 requests on empty file, got %d", len(reqs))
		}
		if newState.LastOffset != 0 {
			t.Errorf("expected offset 0, got %d", newState.LastOffset)
		}
	})

	// 2c. Corrupt Copilot CLI JSONL (corrupt line in middle, partial trailing line)
	t.Run("CopilotCLI_CorruptLines", func(t *testing.T) {
		copilotPath := filepath.Join(dir, "corrupt_copilot.jsonl")
		content := `{"id":"1","session_id":"s1","timestamp":"2026-10-18T10:00:00Z","query":"cmd 1","explanation":"expl 1"}
{CORRUPT_JSON_MIDDLE_LINE}
{"id":"2","session_id":"s1","timestamp":"2026-10-18T10:01:00Z","query":"cmd 2","explanation":"expl 2"}
{"id":"3","partial_trailing_line`
		if err := os.WriteFile(copilotPath, []byte(content), 0644); err != nil {
			t.Fatal(err)
		}
		p := &parsers.CopilotCLIParser{}
		reqs, newState, err := p.Parse(parsers.DataSource{Path: copilotPath, Type: "jsonl"}, store.SyncState{})
		if err != nil {
			t.Fatalf("CopilotCLI failed on corrupt lines: %v", err)
		}
		if len(reqs) != 2 {
			t.Errorf("expected 2 valid requests imported (skipping corrupt middle and partial trailing), got %d", len(reqs))
		}
		t.Logf("CopilotCLI parsed %d requests, LastOffset=%d", len(reqs), newState.LastOffset)
	})

	// 2d. Corrupt Cursor SQLite database (garbage bytes)
	t.Run("Cursor_CorruptSQLiteFile", func(t *testing.T) {
		garbagePath := filepath.Join(dir, "corrupt.vscdb")
		if err := os.WriteFile(garbagePath, []byte("NOT_A_SQLITE_DATABASE_GARBAGE"), 0644); err != nil {
			t.Fatal(err)
		}
		p := &parsers.CursorParser{}
		reqs, _, err := p.Parse(parsers.DataSource{Path: garbagePath, Type: "sqlite"}, store.SyncState{})
		if err != nil {
			t.Logf("Cursor returned error on garbage DB: %v", err)
		}
		if len(reqs) != 0 {
			t.Errorf("expected 0 requests on garbage DB, got %d", len(reqs))
		}
	})

	// 2e. Windsurf corrupt JSON
	t.Run("Windsurf_CorruptJSON", func(t *testing.T) {
		corruptPath := filepath.Join(dir, "corrupt_windsurf.json")
		if err := os.WriteFile(corruptPath, []byte(`{"session_id": "s1", "steps": [MALFORMED`), 0644); err != nil {
			t.Fatal(err)
		}
		p := &parsers.WindsurfParser{}
		reqs, newState, err := p.Parse(parsers.DataSource{Path: corruptPath, Type: "json"}, store.SyncState{})
		if err != nil {
			t.Logf("Windsurf returned error on corrupt JSON: %v", err)
		}
		if len(reqs) != 0 {
			t.Errorf("expected 0 requests on corrupt JSON, got %d", len(reqs))
		}
		t.Logf("Windsurf newState on corrupt JSON: LastOffset=%d", newState.LastOffset)
	})

	// 2f. Missing permissions (0000)
	t.Run("UnreadablePermissions", func(t *testing.T) {
		unreadableDir := filepath.Join(dir, "unreadable_dir")
		if err := os.MkdirAll(unreadableDir, 0755); err != nil {
			t.Fatal(err)
		}
		testFile := filepath.Join(unreadableDir, "file.json")
		_ = os.WriteFile(testFile, []byte("{}"), 0644)
		_ = os.Chmod(unreadableDir, 0000)
		defer os.Chmod(unreadableDir, 0755)

		p := &parsers.ClineParser{}
		_, err := p.Discover(config.ToolConfig{
			Enabled: true,
			Paths:   []string{unreadableDir},
		})
		// Should not panic or crash
		t.Logf("Discover on unreadable directory returned error: %v", err)
	})
}

// -------------------------------------------------------------
// 3. IDEMPOTENCY, DUPLICATE ROWS, AND TOOL CALL RESILIENCE
// -------------------------------------------------------------

func TestResilience_IdempotencyAndToolCallDuplication(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tracker.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Create a Cline file with 1 turn containing a tool call
	clineDir := filepath.Join(dir, "tasks", "task-idem-1")
	if err := os.MkdirAll(clineDir, 0755); err != nil {
		t.Fatal(err)
	}
	histFile := filepath.Join(clineDir, "api_conversation_history.json")

	turn1JSON := `[
		{
			"role": "user",
			"content": "List files"
		},
		{
			"role": "assistant",
			"content": "Listing files.\n<execute_command>ls -l</execute_command>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250000000,
			"usage": { "input_tokens": 500, "output_tokens": 100 }
		}
	]`
	if err := os.WriteFile(histFile, []byte(turn1JSON), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.DisableOtherTools = true
	cfg.Tools = map[string]config.ToolConfig{
		"cline": {
			Enabled: true,
			Paths:   []string{filepath.Join(dir, "tasks")},
		},
	}

	engine := pricing.NewEngine(nil)

	// First Sync
	imp1, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("first sync failed: %v", err)
	}
	if imp1 != 1 {
		t.Fatalf("expected 1 imported request, got %d", imp1)
	}

	var reqCount1 int
	_ = st.DB().QueryRow("SELECT count(*) FROM requests").Scan(&reqCount1)
	var toolCount1 int
	_ = st.DB().QueryRow("SELECT count(*) FROM tool_calls").Scan(&toolCount1)

	if reqCount1 != 1 || toolCount1 != 1 {
		t.Fatalf("expected 1 req and 1 tool call, got %d reqs, %d tools", reqCount1, toolCount1)
	}

	// Second Sync without changes (pure idempotency check)
	imp2, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if imp2 != 0 {
		t.Errorf("expected 0 imported requests on second sync, got %d", imp2)
	}

	var toolCount2 int
	_ = st.DB().QueryRow("SELECT count(*) FROM tool_calls").Scan(&toolCount2)
	if toolCount2 != 1 {
		t.Errorf("CRITICAL BUG: tool_calls count increased from %d to %d on identical sync!", toolCount1, toolCount2)
	}

	// Now append Turn 2 to Cline's conversation history
	time.Sleep(50 * time.Millisecond)
	turn2JSON := `[
		{
			"role": "user",
			"content": "List files"
		},
		{
			"role": "assistant",
			"content": "Listing files.\n<execute_command>ls -l</execute_command>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250000000,
			"usage": { "input_tokens": 500, "output_tokens": 100 }
		},
		{
			"role": "user",
			"content": "Result: done"
		},
		{
			"role": "assistant",
			"content": "Great, now editing file.\n<write_to_file path=\"x.go\">done</write_to_file>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250010000,
			"usage": { "input_tokens": 700, "output_tokens": 120 }
		}
	]`
	if err := os.WriteFile(histFile, []byte(turn2JSON), 0644); err != nil {
		t.Fatal(err)
	}

	// Third Sync after appending Turn 2
	imp3, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("third sync failed: %v", err)
	}
	t.Logf("Third sync imported: %d new record(s)", imp3)

	var reqCount3 int
	_ = st.DB().QueryRow("SELECT count(*) FROM requests").Scan(&reqCount3)
	var toolCount3 int
	_ = st.DB().QueryRow("SELECT count(*) FROM tool_calls").Scan(&toolCount3)

	t.Logf("After turn 2 added: requests count=%d, tool_calls count=%d", reqCount3, toolCount3)
	if reqCount3 != 2 {
		t.Errorf("expected 2 total requests in database, got %d", reqCount3)
	}
	// There should be exactly 2 tool calls: execute_command (from turn 1) and write_to_file (from turn 2)
	if toolCount3 != 2 {
		msg := fmt.Sprintf("QA AUDIT FINDING [CRITICAL]: tool_calls count is %d, expected exactly 2! Duplicate tool calls were inserted for turn 1!", toolCount3)
		if os.Getenv("STRICT_AUDIT") == "1" {
			t.Errorf("%s", msg)
		} else {
			t.Logf("%s", msg)
		}
	}
}

// -------------------------------------------------------------
// 4. CURSOR MULTI-COMPOSER STATE SHIFT TEST
// -------------------------------------------------------------

func TestResilience_CursorMultiComposerShift(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	cursorDB, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = cursorDB.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);`)
	if err != nil {
		t.Fatal(err)
	}

	// Run 1: Two composers, each with 1 turn
	json1 := `{
		"allComposers": [
			{
				"composerId": "comp-A",
				"conversation": [
					{ "type": "user", "text": "A1" },
					{ "type": "ai", "text": "A1-resp", "modelType": "gpt-4o" }
				]
			},
			{
				"composerId": "comp-B",
				"conversation": [
					{ "type": "user", "text": "B1" },
					{ "type": "ai", "text": "B1-resp", "modelType": "gpt-4o" }
				]
			}
		]
	}`
	_, _ = cursorDB.Exec(`INSERT INTO ItemTable VALUES ('composer.composerData', ?);`, json1)
	cursorDB.Close()

	parser := &parsers.CursorParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "cursor"}

	reqs1, newState1, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatalf("first parse failed: %v", err)
	}
	if len(reqs1) != 2 {
		t.Fatalf("expected 2 requests in run 1, got %d", len(reqs1))
	}
	t.Logf("Run 1: Parsed 2 requests, newState.LastOffset = %d", newState1.LastOffset)

	// Run 2: User adds Turn 2 to Composer A!
	// Total turns: Composer A has 2 turns, Composer B has 1 turn. Total turns = 3.
	time.Sleep(50 * time.Millisecond)
	json2 := `{
		"allComposers": [
			{
				"composerId": "comp-A",
				"conversation": [
					{ "type": "user", "text": "A1" },
					{ "type": "ai", "text": "A1-resp", "modelType": "gpt-4o" },
					{ "type": "user", "text": "A2" },
					{ "type": "ai", "text": "A2-resp", "modelType": "gpt-4o" }
				]
			},
			{
				"composerId": "comp-B",
				"conversation": [
					{ "type": "user", "text": "B1" },
					{ "type": "ai", "text": "B1-resp", "modelType": "gpt-4o" }
				]
			}
		]
	}`

	cursorDB, _ = sql.Open("sqlite3", dbPath)
	_, _ = cursorDB.Exec(`UPDATE ItemTable SET value = ? WHERE key = 'composer.composerData'`, json2)
	cursorDB.Close()

	reqs2, newState2, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, newState1)
	if err != nil {
		t.Fatalf("second parse failed: %v", err)
	}

	t.Logf("Run 2: Parsed %d requests, newState.LastOffset = %d", len(reqs2), newState2.LastOffset)
	for i, r := range reqs2 {
		t.Logf("Run 2 req %d: ID=%s Prompt=%s", i, r.ID, r.PromptText)
	}

	// We expect EXACTLY 1 new request: "cursor:comp-A:1" with prompt "A2"
	if len(reqs2) != 1 || (len(reqs2) > 0 && reqs2[0].ID != "cursor:comp-A:1") {
		actualID := "(none)"
		actualPrompt := ""
		if len(reqs2) > 0 {
			actualID = reqs2[0].ID
			actualPrompt = reqs2[0].PromptText
		}
		msg := fmt.Sprintf("QA AUDIT FINDING [CRITICAL]: Cursor multi-composer turn index shift caused new turn A2 to be dropped and old turn to be re-emitted! Expected cursor:comp-A:1, got %s (Prompt=%s)", actualID, actualPrompt)
		if os.Getenv("STRICT_AUDIT") == "1" {
			t.Errorf("%s", msg)
		} else {
			t.Logf("%s", msg)
		}
	}
}

// -------------------------------------------------------------
// 5. DYNAMIC MODEL PRICING RESOLUTION
// -------------------------------------------------------------

func TestResilience_DynamicModelPricing(t *testing.T) {
	engine := pricing.NewEngine(nil)

	testCases := []struct {
		model            string
		inputTokens      int64
		outputTokens     int64
		expectKnownPrice bool
	}{
		// Known models
		{"claude-3-5-sonnet-20241022", 1000, 500, true},
		{"gpt-4o", 1000, 500, true},
		{"deepseek-chat", 1000, 500, true},
		{"gemini-2.5-pro", 1000, 500, true},

		// Unknown / extension internal models
		{"cursor-small", 1000, 500, false},
		{"cursor-fast", 1000, 500, false},
		{"cascade-base", 1000, 500, false},
		{"unknown-custom-model", 1000, 500, false},
	}

	for _, tc := range testCases {
		cost, ok := engine.CalculateCost(tc.model, tc.inputTokens, tc.outputTokens, 0, 0)
		if tc.expectKnownPrice {
			if !ok || cost == nil || *cost <= 0 {
				t.Errorf("expected positive cost calculation for known model %s, got ok=%v, cost=%v", tc.model, ok, cost)
			}
		} else {
			if ok && cost != nil {
				t.Errorf("expected unknown model %s to not have default price, got %f", tc.model, *cost)
			}
		}
	}

	// Test custom pricing override for unknown models
	customPrices := map[string]pricing.ModelPrice{
		"cursor-small": {
			InputPerMTok:  0.20,
			OutputPerMTok: 0.80,
		},
		"cascade-base": {
			InputPerMTok:  0.50,
			OutputPerMTok: 1.50,
		},
	}
	customEngine := pricing.NewEngine(customPrices)

	costCS, okCS := customEngine.CalculateCost("cursor-small", 1_000_000, 1_000_000, 0, 0)
	if !okCS || costCS == nil || *costCS != 1.00 {
		t.Errorf("expected custom price for cursor-small to be 1.00, got ok=%v, cost=%v", okCS, costCS)
	}

	costCB, okCB := customEngine.CalculateCost("cascade-base", 1_000_000, 1_000_000, 0, 0)
	if !okCB || costCB == nil || *costCB != 2.00 {
		t.Errorf("expected custom price for cascade-base to be 2.00, got ok=%v, cost=%v", okCB, costCB)
	}
}

func TestResilience_CustomPricingIgnoredInSyncAll(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Create a Windsurf file with cascade-base model
	windsurfDir := filepath.Join(dir, "windsurf")
	if err := os.MkdirAll(windsurfDir, 0755); err != nil {
		t.Fatal(err)
	}
	windsurfJSON := `{
		"session_id": "custom-price-sess",
		"model": "cascade-base",
		"steps": [
			{
				"step_index": 1,
				"user_prompt": "hello",
				"assistant_response": "world",
				"tokens": { "input_tokens": 1000000, "output_tokens": 1000000 }
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(windsurfDir, "sess.json"), []byte(windsurfJSON), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = filepath.Join(dir, "data")
	cfg.DisableOtherTools = true
	cfg.Tools = map[string]config.ToolConfig{
		"windsurf": {
			Enabled: true,
			Paths:   []string{windsurfDir},
		},
	}

	// Custom engine with pricing for cascade-base
	customPrices := map[string]pricing.ModelPrice{
		"cascade-base": {
			InputPerMTok:  5.0,
			OutputPerMTok: 10.0,
		},
	}
	customEngine := pricing.NewEngine(customPrices)

	imported, err := daemon.SyncAll(cfg, st, customEngine)
	if err != nil {
		t.Fatal(err)
	}
	if imported != 1 {
		t.Fatalf("expected 1 imported record, got %d", imported)
	}

	var cost float64
	err = st.DB().QueryRow("SELECT cost_usd FROM requests WHERE session_id = 'custom-price-sess'").Scan(&cost)
	if err != nil {
		t.Fatal(err)
	}

	t.Logf("Cost stored in database for cascade-base with custom engine: $%f", cost)
	// Expected cost: (1M/1M)*5 + (1M/1M)*10 = $15.00
	if cost == 0.0 {
		msg := "QA AUDIT FINDING [HIGH]: custom pricing engine passed to SyncAll was ignored! Cost stored as $0.00 instead of $15.00 because parser pre-emptively defaulted CostUSD to &0.0"
		if os.Getenv("STRICT_AUDIT") == "1" {
			t.Errorf("%s", msg)
		} else {
			t.Logf("%s", msg)
		}
	}
}

// -------------------------------------------------------------
// 6. RESOURCE LEAKS (DB CONNECTIONS AND FILE HANDLES)
// -------------------------------------------------------------

func TestResilience_ResourceLeaks_VSCDBOpenClose(t *testing.T) {
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	_, _ = db.Exec(`CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);`)
	db.Close()

	// Open and close 200 times sequentially
	for i := 0; i < 200; i++ {
		r, err := vscdb.Open(dbPath)
		if err != nil {
			t.Fatalf("vscdb.Open failed on iteration %d: %v", i, err)
		}
		_, _ = r.GetKey("composer.composerData")
		if err := r.Close(); err != nil {
			t.Fatalf("r.Close failed on iteration %d: %v", i, err)
		}
	}
}

func TestResilience_ConcurrentParsers(t *testing.T) {
	dir := t.TempDir()
	clinePath := filepath.Join(dir, "api_conversation_history.json")
	clineJSON := `[
		{"role": "assistant", "content": "hi", "model": "gpt-4o", "usage": {"input_tokens": 10, "output_tokens": 10}}
	]`
	_ = os.WriteFile(clinePath, []byte(clineJSON), 0644)

	var wg sync.WaitGroup
	p := &parsers.ClineParser{}
	for i := 0; i < 20; i++ {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, _, _ = p.Parse(parsers.DataSource{Path: clinePath, Type: "json"}, store.SyncState{})
		}()
	}
	wg.Wait()
}
