package test

import (
	"bytes"
	"database/sql"
	"os"
	"os/exec"
	"path/filepath"
	"strings"
	"testing"

	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	"gopkg.in/yaml.v3"
)

// setupAllAdapterFixtures creates mock directory structures and fixtures for all 10 adapters.
func setupAllAdapterFixtures(t *testing.T, baseDir string) *config.Config {
	t.Helper()

	cfg := config.DefaultConfig()
	cfg.DataDir = filepath.Join(baseDir, "data")
	cfg.StoreContent = true

	// 1. Cline fixture
	clineDir := filepath.Join(baseDir, "cline", "tasks", "task-cline-e2e")
	if err := os.MkdirAll(clineDir, 0755); err != nil {
		t.Fatal(err)
	}
	clineJSON := `[
		{
			"role": "user",
			"content": "Create a file main.go"
		},
		{
			"role": "assistant",
			"content": "Writing main.go file.\n<write_to_file path=\"main.go\">package main</write_to_file>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250000000,
			"usage": {
				"input_tokens": 1000,
				"output_tokens": 200,
				"cache_creation_input_tokens": 3000,
				"cache_read_input_tokens": 1500
			}
		},
		{
			"role": "user",
			"content": "[write_to_file] Result: Success"
		},
		{
			"role": "assistant",
			"content": "File created.\n<attempt_completion>All tasks completed.</attempt_completion>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250005000,
			"usage": {
				"input_tokens": 1200,
				"output_tokens": 150,
				"cache_read_input_tokens": 4000
			}
		}
	]`
	if err := os.WriteFile(filepath.Join(clineDir, "api_conversation_history.json"), []byte(clineJSON), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["cline"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{filepath.Join(baseDir, "cline", "tasks")},
	}

	// 2. Roo Code fixture
	rooDir := filepath.Join(baseDir, "roo_code", "tasks", "task-roo-e2e")
	if err := os.MkdirAll(rooDir, 0755); err != nil {
		t.Fatal(err)
	}
	rooJSON := `[
		{
			"role": "user",
			"content": "Read configuration"
		},
		{
			"role": "assistant",
			"content": "Reading config.\n<read_file path=\"config.yaml\"></read_file>",
			"model": "claude-3-5-sonnet-20241022",
			"ts": 1729250010000,
			"usage": {
				"input_tokens": 800,
				"output_tokens": 100,
				"cache_read_input_tokens": 2000
			}
		}
	]`
	if err := os.WriteFile(filepath.Join(rooDir, "api_conversation_history.json"), []byte(rooJSON), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["roo_code"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{filepath.Join(baseDir, "roo_code", "tasks")},
	}

	// 3. Cursor fixture (state.vscdb with composerData)
	cursorDir := filepath.Join(baseDir, "cursor", "workspaceStorage", "w1")
	if err := os.MkdirAll(cursorDir, 0755); err != nil {
		t.Fatal(err)
	}
	cursorDBPath := filepath.Join(cursorDir, "state.vscdb")
	cursorDB, err := sql.Open("sqlite3", cursorDBPath)
	if err != nil {
		t.Fatal(err)
	}
	cursorJSON := `{
		"allComposers": [
			{
				"composerId": "cursor-sess-e2e",
				"conversation": [
					{ "type": "user", "text": "Run build" },
					{
						"type": "ai",
						"text": "Building now",
						"modelType": "claude-3-5-sonnet",
						"inputTokens": 500,
						"outputTokens": 100,
						"actions": [
							{ "type": "run_command", "command": "go build", "status": "success" }
						]
					}
				]
			}
		]
	}`
	_, err = cursorDB.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', ?);
	`, cursorJSON)
	cursorDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools["cursor"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{filepath.Join(baseDir, "cursor", "workspaceStorage")},
	}

	// 4. Windsurf fixture (cascade json)
	windsurfDir := filepath.Join(baseDir, "windsurf", "cascade")
	if err := os.MkdirAll(windsurfDir, 0755); err != nil {
		t.Fatal(err)
	}
	windsurfJSON := `{
		"session_id": "windsurf-sess-e2e",
		"model": "claude-3-5-sonnet",
		"steps": [
			{
				"step_index": 1,
				"timestamp": "2026-10-18T10:00:00Z",
				"user_prompt": "Refactor auth module",
				"assistant_response": "Refactored auth module successfully.",
				"duration_ms": 1100,
				"tokens": { "input_tokens": 900, "output_tokens": 150 },
				"tool_calls": [
					{ "id": "tc-1", "name": "edit_code", "status": "success" }
				]
			},
			{
				"step_index": 2,
				"timestamp": "2026-10-18T10:00:10Z",
				"user_prompt": "Run integration tests",
				"assistant_response": "Tests failed with exit code 1",
				"duration_ms": 2500,
				"tokens": { "input_tokens": 1200, "output_tokens": 80 },
				"tool_calls": [
					{ "id": "tc-2", "name": "run_tests", "status": "error" }
				]
			}
		]
	}`
	if err := os.WriteFile(filepath.Join(windsurfDir, "session-e2e.json"), []byte(windsurfJSON), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["windsurf"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{windsurfDir},
	}

	// 5. Copilot CLI fixture (JSONL)
	copilotDir := filepath.Join(baseDir, "copilot_cli")
	if err := os.MkdirAll(copilotDir, 0755); err != nil {
		t.Fatal(err)
	}
	copilotJSONL := `{"id":"c1","session_id":"copilot-sess-e2e","timestamp":"2026-10-18T11:00:00Z","command_type":"explain","query":"explain kubectl get pods","explanation":"Lists all pods in the current namespace.","model":"gpt-4o"}
{"id":"c2","session_id":"copilot-sess-e2e","timestamp":"2026-10-18T11:05:00Z","command_type":"suggest","query":"kill process on port 8080","suggestion":"lsof -ti:8080 | xargs kill -9","explanation":"Finds PID and sends SIGKILL."}
`
	if err := os.WriteFile(filepath.Join(copilotDir, "history.jsonl"), []byte(copilotJSONL), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["copilot_cli"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{copilotDir},
	}

	// 6. Claude Code fixture
	claudeDir := filepath.Join(baseDir, "claude", "projects", "p1")
	if err := os.MkdirAll(claudeDir, 0755); err != nil {
		t.Fatal(err)
	}
	claudeJSONL := `{"type":"user","message":{"role":"user","content":"hello claude"}}
{"type":"assistant","costUSD":0.015,"durationMs":1200,"message":{"model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":1000,"output_tokens":100,"cache_read_input_tokens":500,"cache_creation_input_tokens":0}}}
`
	if err := os.WriteFile(filepath.Join(claudeDir, "session.jsonl"), []byte(claudeJSONL), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["claude_code"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{filepath.Join(baseDir, "claude", "projects")},
	}

	// 7. AGY fixture
	agyDir := filepath.Join(baseDir, "agy", "conversations")
	if err := os.MkdirAll(agyDir, 0755); err != nil {
		t.Fatal(err)
	}
	agyJSONL := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-17T12:00:00Z","content":"run agy task"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:05Z","input_tokens":2000,"output_tokens":300,"cache_read_tokens":1000}
`
	if err := os.WriteFile(filepath.Join(agyDir, "agy-sess.jsonl"), []byte(agyJSONL), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["agy"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{filepath.Join(baseDir, "agy", "conversations")},
	}

	// 8. Aider fixture
	aiderDir := filepath.Join(baseDir, "aider")
	if err := os.MkdirAll(aiderDir, 0755); err != nil {
		t.Fatal(err)
	}
	aiderFile := filepath.Join(aiderDir, "analytics.jsonl")
	aiderJSONL := `{"event":"message_send","properties":{"main_model":"anthropic/claude-3-5-sonnet-20241022","prompt_tokens":5000,"completion_tokens":200,"total_tokens":5200,"cost":0.025},"time":1742560429}
`
	if err := os.WriteFile(aiderFile, []byte(aiderJSONL), 0644); err != nil {
		t.Fatal(err)
	}
	cfg.Tools["aider"] = config.ToolConfig{
		Enabled:      true,
		AnalyticsLog: aiderFile,
	}

	// 9. Codex fixture (SQLite)
	codexDir := filepath.Join(baseDir, "codex")
	if err := os.MkdirAll(codexDir, 0755); err != nil {
		t.Fatal(err)
	}
	codexDBPath := filepath.Join(codexDir, "state_5.sqlite")
	codexDB, err := sql.Open("sqlite3", codexDBPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = codexDB.Exec(`
		CREATE TABLE threads (id TEXT PRIMARY KEY, model_provider TEXT, tokens_used INTEGER, created_at INTEGER);
		INSERT INTO threads VALUES ('thread-codex-1', 'gpt-4o', 1500, 1729250000000);
	`)
	codexDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools["codex"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{codexDBPath},
	}

	// 10. OpenCode fixture (SQLite)
	opencodeDir := filepath.Join(baseDir, "opencode")
	if err := os.MkdirAll(opencodeDir, 0755); err != nil {
		t.Fatal(err)
	}
	opencodeDBPath := filepath.Join(opencodeDir, "opencode.db")
	ocDB, err := sql.Open("sqlite3", opencodeDBPath)
	if err != nil {
		t.Fatal(err)
	}
	_, err = ocDB.Exec(`
		CREATE TABLE messages (id TEXT PRIMARY KEY, session_id TEXT, model TEXT, tokens_in INTEGER, tokens_out INTEGER, created_at DATETIME);
		INSERT INTO messages VALUES ('oc-msg-1', 'oc-sess-1', 'claude-3-5-sonnet', 800, 150, '2026-10-18 12:00:00');
	`)
	ocDB.Close()
	if err != nil {
		t.Fatal(err)
	}
	cfg.Tools["opencode"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{opencodeDBPath},
	}

	return cfg
}

func TestAll10Adapters_EndToEndPipeline(t *testing.T) {
	baseDir := t.TempDir()
	cfg := setupAllAdapterFixtures(t, baseDir)

	dbPath := filepath.Join(cfg.DataDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to create store: %v", err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	// Step 1: Run SyncAll across ALL 10 adapters in a single sync run
	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("SyncAll error: %v", err)
	}

	t.Logf("SyncAll imported %d records across all 10 adapters", imported)
	if imported < 10 {
		t.Fatalf("expected at least 10 records imported across 10 tools, got %d", imported)
	}

	// Step 2: Verify Overview persistence and metrics
	overview, err := st.GetOverview("all")
	if err != nil {
		t.Fatalf("GetOverview error: %v", err)
	}

	if overview.TotalSessions < 8 {
		t.Errorf("expected at least 8 distinct sessions, got %d", overview.TotalSessions)
	}
	if overview.TotalRequests < 10 {
		t.Errorf("expected at least 10 total requests, got %d", overview.TotalRequests)
	}
	if overview.TotalTokens <= 0 {
		t.Errorf("expected positive total tokens, got %d", overview.TotalTokens)
	}
	if overview.TotalCost <= 0.0 {
		t.Errorf("expected positive total cost, got %f", overview.TotalCost)
	}
	if overview.TotalToolCalls <= 0 {
		t.Errorf("expected positive tool calls recorded, got %d", overview.TotalToolCalls)
	}
	if overview.TotalToolErrors <= 0 {
		t.Errorf("expected at least 1 tool error recorded (from windsurf step 2), got %d", overview.TotalToolErrors)
	}

	// Step 3: Verify Tool Observability metrics
	toolStats, err := st.GetToolCallStats("all")
	if err != nil {
		t.Fatalf("GetToolCallStats error: %v", err)
	}
	if len(toolStats) == 0 {
		t.Fatalf("expected tool call stats populated, got empty slice")
	}

	foundWriteFile := false
	foundReadFile := false
	foundRunTests := false
	foundRunCommand := false
	for _, ts := range toolStats {
		t.Logf("ToolCallStat: Name=%s Total=%d Errors=%d", ts.ToolName, ts.TotalCalls, ts.ErrorCount)
		if ts.ToolName == "write_to_file" {
			foundWriteFile = true
		}
		if ts.ToolName == "read_file" {
			foundReadFile = true
		}
		if ts.ToolName == "run_tests" {
			foundRunTests = true
			if ts.ErrorCount != 1 {
				t.Errorf("expected run_tests to have 1 error, got %d", ts.ErrorCount)
			}
		}
		if ts.ToolName == "run_command" {
			foundRunCommand = true
		}
	}
	if !foundWriteFile {
		t.Errorf("expected write_to_file in tool call stats")
	}
	if !foundReadFile {
		t.Errorf("expected read_file in tool call stats")
	}
	if !foundRunTests {
		t.Errorf("expected run_tests in tool call stats")
	}
	if !foundRunCommand {
		t.Errorf("expected run_command in tool call stats")
	}

	// Step 4: Verify Prompt Cache Analytics
	cacheStats, err := st.GetCacheStats("all")
	if err != nil {
		t.Fatalf("GetCacheStats error: %v", err)
	}
	if cacheStats.CacheHitTokens <= 0 {
		t.Errorf("expected positive cache hit tokens, got %d", cacheStats.CacheHitTokens)
	}
	if cacheStats.CacheHitRate <= 0 {
		t.Errorf("expected positive cache hit rate, got %f", cacheStats.CacheHitRate)
	}
	if cacheStats.SavingsUSD <= 0 {
		t.Errorf("expected positive cache savings USD, got %f", cacheStats.SavingsUSD)
	}

	// Step 5: Verify Session Summaries and Detailed Turns
	summaries, err := st.GetSessionSummaries("all", 50)
	if err != nil {
		t.Fatalf("GetSessionSummaries error: %v", err)
	}

	toolSet := make(map[string]bool)
	for _, sm := range summaries {
		toolSet[sm.Tool] = true
	}

	for _, expectedTool := range []string{"cline", "roo_code", "cursor", "windsurf", "copilot_cli"} {
		if !toolSet[expectedTool] {
			t.Errorf("expected tool %s to be represented in session summaries, found: %v", expectedTool, toolSet)
		}
	}

	// Inspect windsurf session detail specifically
	detail, err := st.GetSessionDetail("windsurf-sess-e2e")
	if err != nil {
		t.Fatalf("GetSessionDetail(windsurf-sess-e2e) error: %v", err)
	}
	if detail.Summary.TurnCount != 2 {
		t.Errorf("expected 2 turns in windsurf session, got %d", detail.Summary.TurnCount)
	}
	if detail.Summary.ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error in windsurf session summary, got %d", detail.Summary.ToolErrorCount)
	}
	if len(detail.Turns) != 2 {
		t.Fatalf("expected 2 turn records in detail, got %d", len(detail.Turns))
	}
	if detail.Turns[1].ToolErrorCount != 1 {
		t.Errorf("expected 1 tool error on windsurf turn 2, got %d", detail.Turns[1].ToolErrorCount)
	}
}

func TestAll10Adapters_CLICommands(t *testing.T) {
	// Build cmd/llm-track binary
	tmpDir := t.TempDir()
	binPath := filepath.Join(tmpDir, "llm-track")

	buildCmd := exec.Command("go", "build", "-o", binPath, "../cmd/llm-track")
	buildCmd.Dir = "."
	buildOut, err := buildCmd.CombinedOutput()
	if err != nil {
		t.Fatalf("failed to build llm-track binary: %v\nOutput: %s", err, string(buildOut))
	}

	// Setup fixtures and config file
	fixturesBase := filepath.Join(tmpDir, "fixtures")
	cfg := setupAllAdapterFixtures(t, fixturesBase)

	cfgFilePath := filepath.Join(tmpDir, "config.yaml")
	cfgBytes, err := yaml.Marshal(cfg)
	if err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(cfgFilePath, cfgBytes, 0644); err != nil {
		t.Fatal(err)
	}

	// Helper to run CLI command
	runCLI := func(args ...string) (string, error) {
		fullArgs := append(args, "--config", cfgFilePath)
		cmd := exec.Command(binPath, fullArgs...)
		var out bytes.Buffer
		cmd.Stdout = &out
		cmd.Stderr = &out
		err := cmd.Run()
		return out.String(), err
	}

	// 1. Test `llm-track tools`
	t.Run("CLI_tools", func(t *testing.T) {
		out, err := runCLI("tools")
		if err != nil {
			t.Fatalf("llm-track tools failed: %v, out: %s", err, out)
		}
		for _, tool := range []string{"cline", "roo_code", "cursor", "windsurf", "copilot_cli"} {
			if !strings.Contains(out, tool) {
				t.Errorf("expected tool %s in tools output:\n%s", tool, out)
			}
		}
	})

	// 2. Test `llm-track sync`
	t.Run("CLI_sync", func(t *testing.T) {
		out, err := runCLI("sync")
		if err != nil {
			t.Fatalf("llm-track sync failed: %v, out: %s", err, out)
		}
		if !strings.Contains(out, "Sync complete.") {
			t.Errorf("expected 'Sync complete.' in sync output:\n%s", out)
		}
		if strings.Contains(out, "Imported 0 new records") {
			t.Errorf("expected positive records imported on initial sync:\n%s", out)
		}
	})

	// 3. Test `llm-track summary --period all`
	t.Run("CLI_summary", func(t *testing.T) {
		out, err := runCLI("summary", "--period", "all")
		if err != nil {
			t.Fatalf("llm-track summary failed: %v, out: %s", err, out)
		}
		for _, expectedHeader := range []string{"Total Sessions:", "Total Turns:", "Total Tokens:", "Cache Hit Rate:", "Total Cost:", "Top Agentic Tools:"} {
			if !strings.Contains(out, expectedHeader) {
				t.Errorf("expected %q in summary output:\n%s", expectedHeader, out)
			}
		}
	})

	// 4. Test `llm-track sessions --period all`
	t.Run("CLI_sessions_list", func(t *testing.T) {
		out, err := runCLI("sessions", "--period", "all")
		if err != nil {
			t.Fatalf("llm-track sessions failed: %v, out: %s", err, out)
		}
		if !strings.Contains(out, "Recent Sessions — all") {
			t.Errorf("expected header in sessions list output:\n%s", out)
		}
		for _, tool := range []string{"cline", "roo_code", "cursor", "windsurf", "copilot_cli"} {
			if !strings.Contains(out, tool) {
				t.Errorf("expected tool %s in sessions list output:\n%s", tool, out)
			}
		}
	})

	// 5. Test `llm-track sessions <id>` detail
	t.Run("CLI_sessions_detail", func(t *testing.T) {
		out, err := runCLI("sessions", "windsurf-sess-e2e")
		if err != nil {
			t.Fatalf("llm-track sessions <id> failed: %v, out: %s", err, out)
		}
		if !strings.Contains(out, "Session Detail: windsurf-sess-e2e") {
			t.Errorf("expected header in session detail output:\n%s", out)
		}
		if !strings.Contains(out, "run_tests(err)") && !strings.Contains(out, "run_tests") {
			t.Errorf("expected tool call run_tests in turn detail output:\n%s", out)
		}
	})

	// 6. Test Idempotency: re-running `llm-track sync` should import 0 records
	t.Run("CLI_sync_idempotent", func(t *testing.T) {
		out, err := runCLI("sync")
		if err != nil {
			t.Fatalf("re-run llm-track sync failed: %v, out: %s", err, out)
		}
		if !strings.Contains(out, "Imported 0 new record(s)") {
			t.Errorf("expected 'Imported 0 new record(s)' on re-run, got:\n%s", out)
		}
	})
}
