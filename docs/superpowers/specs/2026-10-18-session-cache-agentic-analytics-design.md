# Specification: Session, Cache & Agentic Analytics Suite

**Date:** 2026-10-18  
**Status:** Approved for Implementation  
**Target:** `llm-cost-tracker` (`llm-track`)

---

## 1. Overview & Objectives

`llm-cost-tracker` currently tracks raw token counts (input/output) and estimated USD costs per interaction. Modern AI coding assistants (such as Antigravity/AGY, Claude Code, Codex, and Aider) exhibit rich execution semantics:
1. **Multi-turn Sessions:** Interactions happen in context-carrying sessions with distinct lifecycles, turn counts, and step progressions.
2. **Prompt Caching:** Significant portions of input tokens are served from cache at reduced latency and cost ($T_{\text{hit}}$ vs $T_{\text{miss}}$).
3. **Agentic Tool Execution:** LLMs generate tool calls (shell commands, file reads/writes, subagent launches) whose success/failure rates and frequencies reflect developer workflow and agent reliability.

This feature extends `llm-cost-tracker` to ingest, store, aggregate, and visualize:
- **Token Cache Analytics:** Cache hit tokens ($T_{\text{hit}}$), cache miss tokens ($T_{\text{miss}}$), cache hit rate (%), and dollar cost savings ($).
- **Session Metrics:** Total sessions, turns per session, session duration, and turn-by-turn progression.
- **Agentic Tool Analytics:** Tool call counts, tool breakdown (`run_command`, `view_file`, etc.), tool error counts, and subagent invocations.
- **Enhanced CLI & Dashboard Presentation:** 
  - Rich default `llm-track summary` with session and caching stats.
  - Dedicated `llm-track sessions` CLI commands for list and turn-level drilldowns.
  - Interactive Web Dashboard cards, charts, and Session Explorer table.

---

## 2. Data Model & Schema Migration

### 2.1 Database Schema (`internal/store/schema.go`)

The SQLite schema is upgraded with backwards-compatible additions to `requests` and a new `tool_calls` table:

```sql
-- Upgrades to requests table
ALTER TABLE requests ADD COLUMN step_index INTEGER DEFAULT 0;
ALTER TABLE requests ADD COLUMN tool_call_count INTEGER DEFAULT 0;
ALTER TABLE requests ADD COLUMN tool_error_count INTEGER DEFAULT 0;
ALTER TABLE requests ADD COLUMN subagent_call_count INTEGER DEFAULT 0;

-- Dedicated tool_calls table
CREATE TABLE IF NOT EXISTS tool_calls (
    id          TEXT PRIMARY KEY,
    request_id  TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    status      TEXT DEFAULT 'success', -- 'success' or 'error'
    created_at  DATETIME NOT NULL,
    FOREIGN KEY(request_id) REFERENCES requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_name ON tool_calls(tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_calls_status ON tool_calls(status);
```

### 2.2 Go Models (`internal/store/models.go`)

```go
type Request struct {
    ID                string     `json:"id"`
    Tool              string     `json:"tool"`
    SessionID         string     `json:"session_id"`
    Timestamp         time.Time  `json:"timestamp"`
    Model             string     `json:"model"`
    InputTokens       int64      `json:"input_tokens"`        // Cache Miss
    OutputTokens      int64      `json:"output_tokens"`
    CacheReadTokens   int64      `json:"cache_read_tokens"`   // Cache Hit
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
    Status    string    `json:"status"` // "success" or "error"
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

### 2.3 Migration Execution (`internal/store/store.go`)
During `store.NewStore(path)`:
1. PRAGMA checks verify existing columns in `requests`.
2. Missing columns are appended with `ALTER TABLE requests ADD COLUMN ...`.
3. `tool_calls` table and indices are initialized via `CREATE TABLE IF NOT EXISTS`.

---

## 3. Ingestion & Transcript Parsers

### 3.1 Antigravity Parser (`internal/parsers/agy.go`)
- **Tokens:**
  - `InputTokens` $\rightarrow$ Cache Miss tokens ($T_{\text{miss}}$).
  - `CacheReadTokens` $\rightarrow$ Cache Hit tokens ($T_{\text{hit}}$).
  - `OutputTokens` $\rightarrow$ Generation tokens.
- **Session & Steps:**
  - `sessionID` derived from `/brain/<session-id>/...`.
  - `StepIndex` taken directly from `evt.StepIndex`.
- **Tool Invocations:**
  - Unmarshal `tool_calls` array in `PLANNER_RESPONSE` events:
    - Creates a `store.ToolCall` with `ID: uuid.New().String()`, `ToolName: call.Name`, `Status: "success"`.
    - If `call.Name` in (`invoke_subagent`, `manage_subagents`, `send_message`), increment `SubagentCallCount`.
  - Correlate corresponding execution events (`RUN_COMMAND`, `VIEW_FILE`, etc.):
    - If execution event has `status == "ERROR"` or non-zero `exit_code`, update corresponding tool call status to `"error"` and increment request's `ToolErrorCount`.
- **Batch Insertion:**
  - Store requests and tool calls atomically inside `store.InsertRequestsAndToolCalls(tx, reqs)`.

### 3.2 Claude Code Parser (`internal/parsers/claude_code.go`)
- **Tokens:**
  - `message.usage.input_tokens` $\rightarrow$ Cache Miss.
  - `message.usage.cache_read_input_tokens` $\rightarrow$ Cache Hit.
  - `message.usage.cache_creation_input_tokens` $\rightarrow$ Cache Write.
  - `message.usage.output_tokens` $\rightarrow$ Output tokens.
- **Session & Steps:**
  - `sessionID` from `<uuid>.jsonl` filename.
  - Turn step index maintained sequentially per session.
- **Tool Invocations:**
  - Extracts `tool_use` blocks from assistant content blocks.
  - Correlates with `tool_result` events where `is_error == true` to record error status.

### 3.3 Codex & Aider Parsers (`internal/parsers/codex.go`, `aider.go`)
- **Codex:**
  - Map `threads.id` as `session_id`.
  - Extract tool definitions if available in `thread_dynamic_tools`.
- **Aider:**
  - Treat each `message_send` event as a turn.
  - Derive prompt tokens as cache miss and completion tokens as output.

---

## 4. Analytics Engine & Store Queries

### 4.1 Overview Statistics (`GetOverview`)
Updated query:
```sql
SELECT 
    count(*),
    count(distinct session_id),
    coalesce(sum(total_tokens), 0),
    coalesce(sum(input_tokens), 0),
    coalesce(sum(cache_read_tokens), 0),
    coalesce(sum(output_tokens), 0),
    coalesce(sum(cost_usd), 0.0),
    coalesce(avg(latency_ms), 0.0),
    coalesce(sum(tool_call_count), 0),
    coalesce(sum(tool_error_count), 0),
    coalesce(sum(subagent_call_count), 0)
FROM requests
[WHERE period_clause]
```
Calculations:
- `AvgTurnsPerSession = TotalRequests / TotalSessions` (if `TotalSessions > 0`).
- `CacheHitRate = CacheHitTokens / (CacheHitTokens + CacheMissTokens) * 100`.
- `CacheSavingsUSD` computed per model using pricing differences: $\sum (\text{CacheHitTokens} \times (R_{\text{input}} - R_{\text{cache\_read}}))$.

### 4.2 Session Aggregations
- `GetSessionSummaries(period string, limit int)`:
  Groups requests by `session_id` and `tool`:
  - `start_time = MIN(timestamp)`, `end_time = MAX(timestamp)`.
  - `duration = end_time - start_time`.
  - `turn_count = COUNT(*)`.
  - Computes per-session totals for tokens, costs, cache hit rate, tool calls, tool errors, and subagents.
- `GetSessionDetail(sessionID string)`:
  Returns `SessionSummary`, the ordered slice of `Request` turns, and child `ToolCall` records.

### 4.3 Tool Invocations
- `GetToolCallStats(period string)`:
  Aggregates `tool_calls` joined with `requests` to respect the time period filter:
  - Groups by `tool_name`, counts total calls and failures (`status = 'error'`).

---

## 5. CLI Interface

### 5.1 Enhanced `llm-track summary`
```text
╭─────────────────────────────────────────────────────────────╮
│                   LLM Cost Tracker — today                  │
├─────────────────────────────────────────────────────────────┤
│  Total Sessions:    14                                      │
│  Total Turns:       186 (avg 13.3 turns/session)            │
│  Total Tokens:      1,420,850                               │
│  Cache Hit Rate:    81.2% (1.15M tokens, $1.45 saved)       │
│  Total Cost:        $4.18                                   │
│  Avg Latency:       1.35s                                   │
├─────────────────────────────────────────────────────────────┤
│  Top Agentic Tools:                                         │
│    run_command      142 calls (3 errors)                    │
│    view_file         98 calls (0 errors)                    │
│    replace_content   24 calls (1 error)                     │
│    invoke_subagent    8 calls                               │
├─────────────────────────────────────────────────────────────┤
│  By Tool:                                                   │
│    agy              12 sessions  160 turns   $3.42          │
│    claude_code       2 sessions   26 turns   $0.76          │
╰─────────────────────────────────────────────────────────────╯
```

### 5.2 Dedicated Sessions Command (`llm-track sessions`)
- `llm-track sessions [--period=today|week|month|all] [--limit=20]`
  Tabular overview:
  `SESSION ID | TOOL | TURNS | DURATION | TOKENS | CACHE % | COST | TOOLS (ERR)`
- `llm-track sessions <session-id>`
  Deep-dive into a session:
  - Header: ID, tool, start/end time, total cost, cache savings, tokens.
  - Step-by-step turn table: Step #, timestamp, model, miss/hit/output tokens, latency, cost, tools executed, errors.

---

## 6. Web Dashboard Enhancements

### 6.1 Backend Handlers (`internal/dashboard/handlers.go`)
- `GET /api/overview?period=...`: Extended with sessions, cache hit rate, and savings.
- `GET /api/analytics/cache?period=...`: Returns `CacheStats`.
- `GET /api/analytics/tools?period=...`: Returns `[]ToolCallStat`.
- `GET /api/sessions?period=...&limit=...`: Returns `[]SessionSummary`.
- `GET /api/sessions/:id`: Returns `SessionDetail`.

### 6.2 Frontend Visuals (`internal/dashboard/static/`)
- **Metric Cards:**
  - "Cache Hit Rate" with highlight badge for dollars saved ($).
  - "Sessions" with average turns per session.
- **Charts:**
  - Tool Invocations Doughnut Chart (proportions of `run_command`, `view_file`, etc.).
  - Token Composition Stacked Bar Chart (Prompt Miss vs Prompt Cache Hit vs Generated Output).
- **Interactive Sessions Explorer Table:**
  - Searchable, sortable list of sessions.
  - Clicking any session row displays a slide-out modal or nested view showing every turn step, tokens, and tool calls.

---

## 7. Testing & Verification

1. **Store Tests (`internal/store/`)**:
   - Migration test verifying column addition and `tool_calls` table creation on existing SQLite databases.
   - Comprehensive query unit tests for `GetOverview`, `GetCacheStats`, `GetSessionSummaries`, `GetSessionDetail`, and `GetToolCallStats`.
2. **Parser Tests (`internal/parsers/`)**:
   - AGY parser test verifying extraction of `tool_calls`, error matching, step indices, and token splitting.
   - Claude Code parser test verifying tool extraction and cache read/creation token extraction.
3. **CLI Tests (`internal/cli/`)**:
   - Unit tests for `RenderSummary` formatting with cache and session statistics.
   - Unit tests for `RenderSessionsList` and `RenderSessionDetail`.
4. **Dashboard Tests (`internal/dashboard/`)**:
   - HTTP endpoint integration tests verifying JSON payloads for cache, tools, and session APIs.
5. **System Verification**:
   - Sync live transcripts, verify CLI commands, start dashboard server, and run `go test ./...`.
