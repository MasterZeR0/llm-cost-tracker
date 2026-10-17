# Session, Cache & Agentic Analytics Suite Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Extend `llm-cost-tracker` to ingest, aggregate, and visualize prompt caching efficiency ($T_{\text{hit}}$, $T_{\text{miss}}$, hit rate %, cost savings), multi-turn session lifecycles, and agentic tool invocations across the SQLite store, transcript parsers, CLI commands (`summary` and `sessions`), and the Web Dashboard.

**Architecture:** Add columns to `requests` and a dedicated `tool_calls` table with automatic SQLite migrations. Update AGY and Claude Code parsers to extract tool calls, execution statuses, and step indices. Add store aggregation queries for cache efficiency, session lifecycles, and tool breakdowns. Expose metrics via CLI `llm-track summary`, new `llm-track sessions` commands, and Web Dashboard API endpoints with interactive Chart.js visualizations.

**Tech Stack:** Go 1.22+, SQLite3 (`github.com/mattn/go-sqlite3`), Cobra CLI, HTML5/CSS3/Vanilla JS with Chart.js.

**Spec:** `docs/superpowers/specs/2026-10-18-session-cache-agentic-analytics-design.md`

## Global Constraints

- Backwards-compatible SQLite schema migrations without data loss.
- Zero external runtime dependencies; use standard library and existing project dependencies.
- Follow existing codebase idioms: Go standard testing, `store.Store`, `parsers.Parser`, and Cobra commands.
- TDD required: tests written and confirmed failing before implementation for each task.

## Review Focus

1. Zero-session / empty database state: `GetOverview`, `GetSessionSummaries`, and `GetCacheStats` must return zero-valued structs without divide-by-zero panics or SQL errors when no rows match the period.
2. Incomplete trailing lines in JSONL transcripts: Parsers must not miscount tool calls or corrupt offsets when transcripts are actively being written.
3. Multi-turn session duration calculation: Sessions with a single turn must have `duration_seconds == 0` without negative durations or overflow.
4. Tool error matching: Failed commands in execution events (`exit_code != 0` or `status == "ERROR"`) must accurately flag the preceding tool invocation as `error`.
5. Non-existent session query: `GetSessionDetail` with an unknown `sessionID` must return a clean `sql.ErrNoRows` / `nil, nil` without crashing.

---

### Task 1: Store Models, Schema & Automatic Migration

**Files:**
- Modify: `internal/store/models.go`
- Modify: `internal/store/schema.go`
- Modify: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Consumes: Existing `Store` and `SchemaSQL`.
- Produces:
  - Extended `Request` with `StepIndex int64`, `ToolCallCount int`, `ToolErrorCount int`, `SubagentCallCount int`, `ToolCalls []ToolCall`.
  - Types `ToolCall`, `SessionSummary`, `SessionDetail`, `CacheStats`, `ToolCallStat`.
  - Method `InsertRequestsWithTools(reqs []Request) error` in `Store`.
  - Migration check in `store.NewStore(path)` ensuring columns and table exist.

- [ ] **Step 1: Write the failing test for schema migration and tool call insertions**

In `internal/store/store_test.go`, add:

```go
func TestStore_MigrationAndToolCalls(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_migration.db")
	st, err := NewStore(dbPath)
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
	req := Request{
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
		ToolCalls: []ToolCall{
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

	if err := st.InsertRequestsWithTools([]Request{req}); err != nil {
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
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestStore_MigrationAndToolCalls ./internal/store`  
Expected: FAIL with compilation error (fields/methods undefined or table missing).

- [ ] **Step 3: Update models, schema, and store implementation**

1. In `internal/store/models.go`, add the new fields and types:
```go
type Request struct {
	ID                string     `json:"id"`
	Tool              string     `json:"tool"`
	SessionID         string     `json:"session_id"`
	Timestamp         time.Time  `json:"timestamp"`
	Model             string     `json:"model"`
	InputTokens       int64      `json:"input_tokens"`
	OutputTokens      int64      `json:"output_tokens"`
	CacheReadTokens   int64      `json:"cache_read_tokens"`
	CacheWriteTokens  int64      `json:"cache_write_tokens"`
	TotalTokens       int64      `json:"total_tokens"`
	CostUSD           *float64   `json:"cost_usd"`
	LatencyMS         *int64     `json:"latency_ms"`
	Status            string     `json:"status"`
	PromptText        string     `json:"prompt_text,omitempty"`
	ResponseText      string     `json:"response_text,omitempty"`
	SourceFile        string     `json:"source_file"`
	RawData           string     `json:"raw_data,omitempty"`
	ImportedAt        time.Time  `json:"imported_at"`
	StepIndex         int64      `json:"step_index"`
	ToolCallCount     int        `json:"tool_call_count"`
	ToolErrorCount    int        `json:"tool_error_count"`
	SubagentCallCount int        `json:"subagent_call_count"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	SessionID string    `json:"session_id"`
	ToolName  string    `json:"tool_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionSummary struct {
	SessionID       string    `json:"session_id"`
	Tool            string    `json:"tool"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	TurnCount       int64     `json:"turn_count"`
	TotalTokens     int64     `json:"total_tokens"`
	CacheHitTokens  int64     `json:"cache_hit_tokens"`
	CacheMissTokens int64     `json:"cache_miss_tokens"`
	CacheHitRate    float64   `json:"cache_hit_rate"`
	TotalCost       float64   `json:"total_cost"`
	ToolCallCount   int64     `json:"tool_call_count"`
	ToolErrorCount  int64     `json:"tool_error_count"`
	SubagentCount   int64     `json:"subagent_count"`
}

type SessionDetail struct {
	Summary SessionSummary `json:"summary"`
	Turns   []Request      `json:"turns"`
}

type CacheStats struct {
	CacheHitTokens  int64   `json:"cache_hit_tokens"`
	CacheMissTokens int64   `json:"cache_miss_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	TotalPrompt     int64   `json:"total_prompt_tokens"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	SavingsUSD      float64 `json:"savings_usd"`
}

type ToolCallStat struct {
	ToolName   string `json:"tool_name"`
	TotalCalls int64  `json:"total_calls"`
	ErrorCount int64  `json:"error_count"`
}
```

2. In `internal/store/schema.go`, add columns to base schema and `tool_calls` table:
```go
CREATE TABLE IF NOT EXISTS tool_calls (
    id          TEXT PRIMARY KEY,
    request_id  TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    status      TEXT DEFAULT 'success',
    created_at  DATETIME NOT NULL,
    FOREIGN KEY(request_id) REFERENCES requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_name ON tool_calls(tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_calls_status ON tool_calls(status);
```

3. In `internal/store/store.go`, add migration checks in `NewStore` and implement `InsertRequestsWithTools`:
```go
func (s *Store) migrate() error {
	cols := map[string]string{
		"step_index":          "INTEGER DEFAULT 0",
		"tool_call_count":     "INTEGER DEFAULT 0",
		"tool_error_count":    "INTEGER DEFAULT 0",
		"subagent_call_count": "INTEGER DEFAULT 0",
	}

	for col, colType := range cols {
		var count int
		_ = s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('requests') WHERE name = ?`, col).Scan(&count)
		if count == 0 {
			_, err := s.db.Exec(`ALTER TABLE requests ADD COLUMN ` + col + ` ` + colType)
			if err != nil {
				return err
			}
		}
	}
	return nil
}
```

Implement `InsertRequestsWithTools(reqs []Request) error` in `internal/store/store.go` to insert `requests` and their `ToolCalls` in a single transaction.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestStore_MigrationAndToolCalls ./internal/store`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): add schema migration and models for sessions, caching, and tool calls"
```

---

### Task 2: Store Analytics Queries & Cache Savings Calculation

**Files:**
- Modify: `internal/store/queries.go`
- Modify: `internal/store/models.go`
- Test: `internal/store/queries_test.go`

**Interfaces:**
- Consumes: `Store`, `Request`, `ToolCall`.
- Produces:
  - `OverviewStats` with session and caching fields.
  - `GetCacheStats(period string) (*CacheStats, error)`.
  - `GetSessionSummaries(period string, limit int) ([]SessionSummary, error)`.
  - `GetSessionDetail(sessionID string) (*SessionDetail, error)`.
  - `GetToolCallStats(period string) ([]ToolCallStat, error)`.

- [ ] **Step 1: Write failing tests for analytics queries**

In `internal/store/queries_test.go`, add:

```go
func TestStore_AnalyticsQueries(t *testing.T) {
	dbPath := filepath.Join(t.TempDir(), "test_queries.db")
	st, err := NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// 1. Verify zero state doesn't error or divide by zero
	overview, err := st.GetOverview("all")
	if err != nil {
		t.Fatalf("unexpected error on empty store: %v", err)
	}
	if overview.TotalSessions != 0 || overview.CacheHitRate != 0.0 {
		t.Fatalf("expected 0 stats on empty store, got %+v", overview)
	}

	cacheStats, err := st.GetCacheStats("all")
	if err != nil {
		t.Fatalf("unexpected error on cache stats: %v", err)
	}
	if cacheStats.TotalPrompt != 0 || cacheStats.CacheHitRate != 0.0 {
		t.Fatalf("expected 0 cache stats, got %+v", cacheStats)
	}

	sessSummaries, err := st.GetSessionSummaries("all", 10)
	if err != nil {
		t.Fatalf("unexpected error on session summaries: %v", err)
	}
	if len(sessSummaries) != 0 {
		t.Fatalf("expected 0 sessions, got %d", len(sessSummaries))
	}

	// 2. Populate test data: 2 sessions, multiple turns, tool calls
	sess1 := "sess-alpha"
	sess2 := "sess-beta"
	now := time.Now().UTC()

	req1 := Request{
		ID:                "req-1",
		Tool:              "agy",
		SessionID:         sess1,
		Timestamp:         now.Add(-10 * time.Minute),
		Model:             "gemini-2-pro",
		InputTokens:       200, // Miss
		CacheReadTokens:   800, // Hit
		OutputTokens:      100,
		TotalTokens:       1100,
		StepIndex:         0,
		ToolCallCount:     1,
		ToolErrorCount:    0,
		SubagentCallCount: 0,
		ToolCalls: []ToolCall{
			{ID: "t-1", RequestID: "req-1", SessionID: sess1, ToolName: "view_file", Status: "success", CreatedAt: now.Add(-10 * time.Minute)},
		},
	}
	req2 := Request{
		ID:                "req-2",
		Tool:              "agy",
		SessionID:         sess1,
		Timestamp:         now.Add(-5 * time.Minute),
		Model:             "gemini-2-pro",
		InputTokens:       100, // Miss
		CacheReadTokens:   900, // Hit
		OutputTokens:      150,
		TotalTokens:       1150,
		StepIndex:         1,
		ToolCallCount:     1,
		ToolErrorCount:    1,
		SubagentCallCount: 0,
		ToolCalls: []ToolCall{
			{ID: "t-2", RequestID: "req-2", SessionID: sess1, ToolName: "run_command", Status: "error", CreatedAt: now.Add(-5 * time.Minute)},
		},
	}
	req3 := Request{
		ID:                "req-3",
		Tool:              "claude_code",
		SessionID:         sess2,
		Timestamp:         now.Add(-2 * time.Minute),
		Model:             "claude-3-7-sonnet",
		InputTokens:       500, // Miss
		CacheReadTokens:   500, // Hit
		OutputTokens:      200,
		TotalTokens:       1200,
		StepIndex:         0,
		ToolCallCount:     1,
		ToolErrorCount:    0,
		SubagentCallCount: 1,
		ToolCalls: []ToolCall{
			{ID: "t-3", RequestID: "req-3", SessionID: sess2, ToolName: "invoke_subagent", Status: "success", CreatedAt: now.Add(-2 * time.Minute)},
		},
	}

	if err := st.InsertRequestsWithTools([]Request{req1, req2, req3}); err != nil {
		t.Fatalf("failed to insert test data: %v", err)
	}

	// 3. Test GetOverview with data
	overview, err = st.GetOverview("all")
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalRequests != 3 || overview.TotalSessions != 2 {
		t.Errorf("expected 3 requests and 2 sessions, got reqs=%d, sess=%d", overview.TotalRequests, overview.TotalSessions)
	}
	if overview.AvgTurnsPerSession < 1.49 || overview.AvgTurnsPerSession > 1.51 {
		t.Errorf("expected avg turns ~1.5, got %f", overview.AvgTurnsPerSession)
	}
	// Total prompt = (200+100+500) miss + (800+900+500) hit = 800 miss + 2200 hit = 3000 total prompt.
	// Hit rate = 2200 / 3000 = 73.33%
	if overview.CacheHitRate < 73.0 || overview.CacheHitRate > 74.0 {
		t.Errorf("expected cache hit rate ~73.33%%, got %f", overview.CacheHitRate)
	}

	// 4. Test GetSessionSummaries
	summaries, err := st.GetSessionSummaries("all", 10)
	if err != nil {
		t.Fatal(err)
	}
	if len(summaries) != 2 {
		t.Fatalf("expected 2 session summaries, got %d", len(summaries))
	}
	if summaries[0].SessionID != sess2 { // sess2 had latest timestamp
		t.Errorf("expected latest session %s first, got %s", sess2, summaries[0].SessionID)
	}
	if summaries[1].SessionID == sess1 {
		if summaries[1].TurnCount != 2 {
			t.Errorf("expected 2 turns for sess1, got %d", summaries[1].TurnCount)
		}
		if summaries[1].DurationSeconds < 299 || summaries[1].DurationSeconds > 301 {
			t.Errorf("expected ~300s duration for sess1, got %f", summaries[1].DurationSeconds)
		}
		if summaries[1].ToolErrorCount != 1 {
			t.Errorf("expected 1 tool error for sess1, got %d", summaries[1].ToolErrorCount)
		}
	}

	// 5. Test GetToolCallStats
	toolStats, err := st.GetToolCallStats("all")
	if err != nil {
		t.Fatal(err)
	}
	if len(toolStats) != 3 {
		t.Fatalf("expected 3 distinct tools, got %d", len(toolStats))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v -run TestStore_AnalyticsQueries ./internal/store`  
Expected: FAIL.

- [ ] **Step 3: Implement queries in `internal/store/queries.go`**

1. Update `OverviewStats` in `internal/store/models.go`:
```go
type OverviewStats struct {
	TotalRequests     int64   `json:"total_requests"`
	TotalSessions     int64   `json:"total_sessions"`
	AvgTurnsPerSession float64 `json:"avg_turns_per_session"`
	TotalTokens       int64   `json:"total_tokens"`
	CacheHitTokens    int64   `json:"cache_hit_tokens"`
	CacheMissTokens   int64   `json:"cache_miss_tokens"`
	CacheHitRate      float64 `json:"cache_hit_rate"`
	CacheSavingsUSD   float64 `json:"cache_savings_usd"`
	TotalCost         float64 `json:"total_cost"`
	AvgLatencyMS      float64 `json:"avg_latency_ms"`
	TotalToolCalls    int64   `json:"total_tool_calls"`
	TotalToolErrors   int64   `json:"total_tool_errors"`
	TotalSubagents    int64   `json:"total_subagents"`
}
```

2. Implement `GetOverview(period string)` in `internal/store/queries.go`:
Query counts, sums of cache hits, misses, tool calls, and compute hit rate & savings safely.

3. Implement `GetCacheStats(period string) (*CacheStats, error)`:
Aggregates hits, misses, outputs, calculates hit rate and dollar savings using default/known rates ($R_{\text{input}} - R_{\text{cache\_read}}$).

4. Implement `GetSessionSummaries(period string, limit int) ([]SessionSummary, error)` and `GetSessionDetail(sessionID string) (*SessionDetail, error)`:
Aggregates by `session_id`, `tool`, with duration calculation: `(julianday(max(timestamp)) - julianday(min(timestamp))) * 86400.0`.

5. Implement `GetToolCallStats(period string) ([]ToolCallStat, error)`:
Groups `tool_calls` by `tool_name`, counts total and errors.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v -run TestStore_AnalyticsQueries ./internal/store`  
Expected: PASS.

- [ ] **Step 5: Run all store tests to ensure zero regressions**

Run: `go test -v ./internal/store`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/store/
git commit -m "feat(store): implement session summaries, cache analytics, and tool call queries"
```

---

### Task 3: Parser Enhancements for AGY & Claude Code

**Files:**
- Modify: `internal/parsers/agy.go`
- Modify: `internal/parsers/claude_code.go`
- Modify: `internal/parsers/agy_test.go`
- Modify: `internal/parsers/claude_code_test.go`

**Interfaces:**
- Consumes: Transcript files (`transcript.jsonl`, `*.jsonl`).
- Produces: `[]store.Request` populated with `StepIndex`, `ToolCallCount`, `ToolErrorCount`, `SubagentCallCount`, and `ToolCalls`.

- [ ] **Step 1: Write failing parser tests for tool calls, errors, and caching**

1. In `internal/parsers/agy_test.go`, add:
```go
func TestAGYParser_ToolCallsAndErrors(t *testing.T) {
	tmpDir := t.TempDir()
	logFile := filepath.Join(tmpDir, "transcript.jsonl")

	content := `{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","content":"list files"}` + "\n" +
		`{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","created_at":"2026-10-18T10:00:00Z","input_tokens":1000,"output_tokens":150,"cache_read_tokens":4000,"tool_calls":[{"name":"run_command","args":{"CommandLine":"ls -la"}},{"name":"invoke_subagent","args":{"Role":"Researcher"}}]}` + "\n" +
		`{"step_index":2,"source":"SYSTEM","type":"RUN_COMMAND","status":"ERROR","created_at":"2026-10-18T10:00:02Z","exit_code":1}` + "\n"

	if err := os.WriteFile(logFile, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	p := &AGYParser{}
	reqs, _, err := p.Parse(DataSource{Path: logFile, Type: "jsonl"}, store.SyncState{SourcePath: logFile, Tool: "agy"})
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
```

2. In `internal/parsers/claude_code_test.go`, add a test for `tool_use` and `tool_result` extraction.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v -run TestAGYParser_ToolCallsAndErrors ./internal/parsers`  
Expected: FAIL.

- [ ] **Step 3: Update `AGYParser.Parse` and `ClaudeCodeParser.Parse`**

1. In `internal/parsers/agy.go`:
   - Extend `agyTranscriptEvent` to parse `ToolCalls []struct { Name string json:"name" } json:"tool_calls"`.
   - When encountering a `PLANNER_RESPONSE`, create `store.ToolCall` entries for each tool call, check for subagent calls (`invoke_subagent`, `manage_subagents`), and buffer them with the request.
   - When encountering execution events (`RUN_COMMAND`, `VIEW_FILE`, etc.), check if `status == "ERROR"` or `exit_code != 0`. If so, mark the corresponding pending tool call as `"error"` and increment `req.ToolErrorCount`.

2. In `internal/parsers/claude_code.go`:
   - Parse `tool_use` content blocks and `tool_result` blocks (`is_error: true`).
   - Populate `ToolCalls`, `ToolCallCount`, and `ToolErrorCount`.

- [ ] **Step 4: Update daemon/sync to save requests with tools**

In `internal/daemon/sync.go` and `internal/cli/sync.go`:
- Ensure `st.InsertRequestsWithTools(requests)` is used when saving requests.

- [ ] **Step 5: Run parser tests to verify they pass**

Run: `go test -v ./internal/parsers`  
Expected: PASS.

- [ ] **Step 6: Commit**

```bash
git add internal/parsers/ internal/daemon/ internal/cli/
git commit -m "feat(parsers): extract tool calls, execution statuses, and step indices"
```

---

### Task 4: CLI Summary & Dedicated Sessions Commands

**Files:**
- Modify: `internal/cli/summary.go`
- Create: `internal/cli/sessions.go`
- Modify: `internal/cli/root.go`
- Modify: `internal/cli/summary_test.go`
- Create: `internal/cli/sessions_test.go`

**Interfaces:**
- Consumes: `store.Store`, `GetOverview`, `GetSessionSummaries`, `GetSessionDetail`, `GetToolCallStats`.
- Produces:
  - Updated `RenderSummary(w io.Writer, s *store.Store, period string) error`.
  - New Cobra command: `sessionsCmd` (`llm-track sessions [--period=...] [--limit=...] [session-id]`).
  - Functions: `RenderSessionsList(w io.Writer, s *store.Store, period string, limit int) error` and `RenderSessionDetail(w io.Writer, s *store.Store, sessionID string) error`.

- [ ] **Step 1: Write failing CLI tests**

1. In `internal/cli/summary_test.go`, assert that `RenderSummary` includes `Total Sessions:`, `Cache Hit Rate:`, and `Top Agentic Tools:`.
2. In `internal/cli/sessions_test.go`, test `RenderSessionsList` and `RenderSessionDetail` formatting.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/cli`  
Expected: FAIL.

- [ ] **Step 3: Implement CLI updates**

1. Update `internal/cli/summary.go`:
   - Fetch `GetOverview(period)` and `GetToolCallStats(period)`.
   - Format box with:
     - `Total Sessions: %d`
     - `Total Turns: %d (avg %.1f / session)`
     - `Cache Hit Rate: %.1f%% (%s tokens, $%.2f saved)`
     - `Top Agentic Tools` section (top 4 tools with call counts and error counts if > 0).

2. Create `internal/cli/sessions.go`:
   - Register `sessionsCmd` with flags `--period` (default "today") and `--limit` (default 20).
   - If argument `[session-id]` is provided, invoke `RenderSessionDetail(os.Stdout, st, args[0])`.
   - Otherwise, invoke `RenderSessionsList(os.Stdout, st, periodFlag, limitFlag)`.
   - Format session table with column widths and human-friendly durations (`12m 45s`).

3. Register `sessionsCmd` in `internal/cli/root.go`:
   - `rootCmd.AddCommand(sessionsCmd)`.

- [ ] **Step 4: Run CLI tests to verify they pass**

Run: `go test -v ./internal/cli`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/cli/
git commit -m "feat(cli): enhance summary with cache and agentic tools, add sessions drilldown"
```

---

### Task 5: Web Dashboard Backend Endpoints & Frontend Visuals

**Files:**
- Modify: `internal/dashboard/handlers.go`
- Modify: `internal/dashboard/server_test.go`
- Modify: `internal/dashboard/static/index.html`
- Modify: `internal/dashboard/static/app.js`
- Modify: `internal/dashboard/static/style.css`

**Interfaces:**
- Consumes: `store.Store`.
- Produces:
  - HTTP Endpoints:
    - `GET /api/overview` (extended with sessions and cache stats)
    - `GET /api/analytics/cache`
    - `GET /api/analytics/tools`
    - `GET /api/sessions`
    - `GET /api/sessions/:id`
  - Frontend Widgets: Cache Hit Rate card, Total Sessions card, Tool Distribution Chart, Token Composition Chart, Sessions Explorer Table with modal drilldown.

- [ ] **Step 1: Write failing dashboard endpoint tests**

In `internal/dashboard/server_test.go`, add tests requesting `/api/analytics/cache`, `/api/analytics/tools`, `/api/sessions`, and `/api/sessions/:id`.

- [ ] **Step 2: Run tests to verify they fail**

Run: `go test -v ./internal/dashboard`  
Expected: FAIL.

- [ ] **Step 3: Implement dashboard handlers and UI**

1. In `internal/dashboard/handlers.go`, implement:
   - `handleCacheAnalytics`: Calls `store.GetCacheStats(period)` and returns JSON.
   - `handleToolAnalytics`: Calls `store.GetToolCallStats(period)` and returns JSON.
   - `handleSessions`: Calls `store.GetSessionSummaries(period, limit)` and returns JSON.
   - `handleSessionDetail`: Parses `:id` from URL path (e.g. `/api/sessions/{id}`), calls `store.GetSessionDetail(id)` and returns JSON.
   - Register routes on multiplexer in `server.go`.

2. In `internal/dashboard/static/index.html`:
   - Add Stat Cards for "Cache Efficiency" (Hit Rate % and Savings $) and "Total Sessions" (Avg Turns).
   - Add chart containers for "Tool Usage Breakdown" and "Token Breakdown".
   - Add "Sessions Explorer" table container and turn drilldown modal.

3. In `internal/dashboard/static/app.js`:
   - Fetch the new endpoints and render Chart.js doughnut chart for tools and stacked bar for tokens.
   - Populate Sessions Explorer table with click handler to load and display session turn chronology.

4. In `internal/dashboard/static/style.css`:
   - Style metric badges, cache efficiency indicators, and the session drilldown modal.

- [ ] **Step 4: Run dashboard tests to verify they pass**

Run: `go test -v ./internal/dashboard`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/dashboard/
git commit -m "feat(dashboard): add cache analytics, tool distribution, and session explorer"
```

---

### Task 6: End-to-End Integration, Live Verification & Build

**Files:**
- Modify: `test/sync_test.go` (or `test/e2e_test.go`)
- Modify: `README.md`

**Interfaces:**
- Validates the complete pipeline: Syncing real transcript logs, querying store, running terminal commands, and verifying dashboard responses.

- [ ] **Step 1: Write end-to-end integration test**

In `test/` package, add an integration test that:
1. Creates a temporary store and syncs sample AGY and Claude transcripts with tool calls.
2. Invokes `cli.RenderSummary` and verifies cache savings and tool counts appear.
3. Invokes `cli.RenderSessionsList` and verifies sessions are listed.
4. Queries dashboard endpoints `/api/overview`, `/api/analytics/tools`, and `/api/sessions`.

- [ ] **Step 2: Run all tests in the workspace**

Run: `go test -v ./...`  
Expected: PASS across all packages.

- [ ] **Step 3: Build binary and perform manual smoke test**

1. Build binary:
   ```bash
   go build -o ./bin/llm-track ./cmd/llm-track
   ```
2. Test CLI commands:
   ```bash
   ./bin/llm-track summary --period=all
   ./bin/llm-track sessions --period=all --limit=5
   ```
3. Update `README.md` with descriptions of the new `sessions` command and cache/agentic analytics features.

- [ ] **Step 4: Final commit**

```bash
git add test/ README.md bin/
git commit -m "feat: complete session, cache, and agentic analytics suite"
```
