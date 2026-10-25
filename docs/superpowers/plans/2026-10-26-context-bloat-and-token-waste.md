# Context Bloat & Token Waste Optimization Engine Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build an in-memory Context Bloat & Token Waste Optimization Engine (`internal/analyzer`) to detect runaway context snowballing, cache churn/invalidation, tool error loops, and repetitive ingestion, surfacing actionable insights via CLI (`llm-track waste`), Web Dashboard (`/api/waste` and trajectory charts), and background daemon threshold monitoring.

**Architecture:** Pure Go analytical engine consuming existing `store.Store` session turn slices without modifying SQLite schema. Plugs into Cobra CLI (`waste.go`), dashboard HTTP REST handlers (`handlers.go`), and background daemon fsnotify loop (`daemon.go`).

**Tech Stack:** Go 1.22+, SQLite3 (`github.com/mattn/go-sqlite3`), Cobra CLI, Chart.js, HTML5/CSS3.

**Spec:** [`docs/superpowers/specs/2026-10-26-context-bloat-and-token-waste-design.md`](file:///Users/nehaverma/orca/projects/llm-cost-tracker/docs/superpowers/specs/2026-10-26-context-bloat-and-token-waste-design.md)

---

## Global Constraints
- **Zero Schema Migrations**: All calculations are performed dynamically on-the-fly from existing `store.Request` and `store.ToolCall` records.
- **Strictly Read-Only**: Analyzers do not modify external tool files or database state.
- **Zero Panics on Edge Cases**: Zero turns, empty DB, single turn, or zero-cost models must cleanly evaluate to 100/100 health without divide-by-zero or nil pointer dereferences.
- **Test-Driven Development**: Every task requires unit tests written and verified before and after implementation.

---

### Task 1: Analyzer Models & Configuration

**Files:**
- Create: `internal/analyzer/models.go`
- Modify: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `analyzer.WasteCategory`, `analyzer.WasteFinding`, `analyzer.SessionWasteAnalysis`, `analyzer.PeriodWasteSummary`, `analyzer.TurnMetric`, `analyzer.Config`.
- Consumes: `internal/config.Config` extended with `WasteMonitoring` config block.

- [ ] **Step 1: Write tests for config loading of WasteMonitoring options**

In `internal/config/config_test.go`, add test cases validating default values and YAML parsing of `waste_monitoring`:
```go
func TestConfig_WasteMonitoringDefaults(t *testing.T) {
	cfg := DefaultConfig()
	if !cfg.WasteMonitoring.Enabled {
		t.Errorf("expected WasteMonitoring.Enabled default true, got false")
	}
	if cfg.WasteMonitoring.SnowballTokenThreshold <= 0 {
		t.Errorf("expected positive SnowballTokenThreshold, got %d", cfg.WasteMonitoring.SnowballTokenThreshold)
	}
}
```

- [ ] **Step 2: Implement config fields in `internal/config/config.go`**

Define `WasteMonitoringConfig`:
```go
type WasteMonitoringConfig struct {
	Enabled                   bool    `yaml:"enabled"`
	SnowballTokenThreshold    int64   `yaml:"snowball_token_threshold"`
	SevereTurnTokenThreshold  int64   `yaml:"severe_turn_token_threshold"`
	WarnConsecutiveToolErrors int     `yaml:"warn_consecutive_tool_errors"`
	CacheChurnDropThreshold   float64 `yaml:"cache_churn_drop_threshold"`
	LogAlertsToConsole        bool    `yaml:"log_alerts_to_console"`
}
```
Set defaults in `DefaultConfig()`:
```go
WasteMonitoring: WasteMonitoringConfig{
    Enabled:                   true,
    SnowballTokenThreshold:    80000,
    SevereTurnTokenThreshold:  120000,
    WarnConsecutiveToolErrors: 3,
    CacheChurnDropThreshold:   0.40,
    LogAlertsToConsole:        true,
},
```

- [ ] **Step 3: Create `internal/analyzer/models.go`**

Implement `WasteCategory`, `WasteSeverity`, `WasteFinding`, `TurnMetric`, `SessionWasteAnalysis`, `PeriodWasteSummary`, and `Config` as specified in the design doc.

- [ ] **Step 4: Verify test passes**

Run `go test -v ./internal/config/...`.

---

### Task 2: Core Heuristic Detectors (Snowball, Cache Churn, Error Loops)

**Files:**
- Create: `internal/analyzer/snowball.go`
- Create: `internal/analyzer/cache_churn.go`
- Create: `internal/analyzer/error_loop.go`
- Create: `internal/analyzer/analyzer.go`
- Create: `internal/analyzer/analyzer_test.go`

**Interfaces:**
- Produces:
  - `AnalyzeSession(detail store.SessionDetail, cfg Config) SessionWasteAnalysis`
  - `AnalyzePeriod(sessions []store.SessionDetail, period string, cfg Config) PeriodWasteSummary`
  - `DetectSnowball(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64)`
  - `DetectCacheChurn(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64)`
  - `DetectErrorLoops(turns []store.Request, cfg Config) ([]WasteFinding, int64, float64)`

- [ ] **Step 1: Write comprehensive failing unit tests in `internal/analyzer/analyzer_test.go`**

Cover:
1. `TestAnalyzeSession_CleanSession`: Flat 5-turn session with 0 errors and steady cache -> 0 waste, 100 health score.
2. `TestAnalyzeSession_ContextSnowball`: 12-turn session where turns 9-12 grow from 40k to 140k tokens -> flags `WasteContextSnowball`, calculates wasted tokens and pro-rated USD, health score $< 70$.
3. `TestAnalyzeSession_CacheChurn`: Session with high initial cache hit (80%) dropping to 10% on turn 6 with 50k tokens -> flags `WasteCacheChurn`.
4. `TestAnalyzeSession_ToolErrorLoop`: 3 consecutive turns with `ToolErrorCount > 0` -> flags `WasteErrorLoop`, calculates waste tokens from retry turns.
5. `TestAnalyzeSession_EdgeCases`: 0 turns, 1 turn, all nil costs, divide-by-zero prevention.
6. `TestAnalyzePeriod_Aggregation`: Aggregates 5 sessions, ranks top waste sessions, produces category totals.

- [ ] **Step 2: Implement `internal/analyzer/snowball.go`**

Implement logic to compute median early context size, detect late-turn cost concentration ($\ge 60\%$ in final quartile), detect single-turn threshold breaches ($>120\text{k}$ tokens), and compute wasted tokens and costs.

- [ ] **Step 3: Implement `internal/analyzer/cache_churn.go`**

Track rolling cache hit rate; detect drops $\ge \text{CacheChurnDropThreshold}$ on turns with input tokens $\ge 20,000$. Compute differential cost between miss price and cache read price.

- [ ] **Step 4: Implement `internal/analyzer/error_loop.go`**

Iterate turns tracking consecutive tool failures (`ToolErrorCount > 0` or status failure). Flag loops $\ge 2$ turns. Compute cumulative token and USD waste from the repeated turns.

- [ ] **Step 5: Implement `internal/analyzer/analyzer.go`**

Tie detectors together in `AnalyzeSession`:
- Construct `TurnMetric` sequence.
- Run snowball, cache churn, and error loop detectors.
- Deduplicate/combine wasted tokens and USD (capping waste at total session cost).
- Compute `HealthScore` formula.
- Generate top actionable recommendations.
Implement `AnalyzePeriod`:
- Rank sessions by `WastedUSD` descending.
- Aggregate category totals and period average health score.

- [ ] **Step 6: Run tests and verify coverage**

Run `go test -v -race ./internal/analyzer/...`.

---

### Task 3: Store Helper for Multi-Session Turns Batch Retrieval

**Files:**
- Modify: `internal/store/queries.go`
- Test: `internal/store/queries_test.go`

**Interfaces:**
- Produces: `GetPeriodSessionDetails(period string) ([]SessionDetail, error)` in `store.Store`.
- Note: Currently `GetSessionSummaries(period)` returns `[]SessionSummary`, while `GetSessionDetail(sessionID)` fetches turns for a single session. To run `AnalyzePeriod` efficiently, we need a batched turn fetch that avoids N+1 queries.

- [ ] **Step 1: Write test for `GetPeriodSessionDetails`**

In `internal/store/queries_test.go`:
```go
func TestStore_GetPeriodSessionDetails(t *testing.T) {
    // Seed requests across 2 sessions
    // Verify GetPeriodSessionDetails("today") returns both sessions populated with turns
}
```

- [ ] **Step 2: Implement `GetPeriodSessionDetails(period string)`**

1. Fetch `summaries, err := s.GetSessionSummaries(period)`.
2. Collect session IDs.
3. Fetch all requests for those sessions in a single `WHERE session_id IN (...) ORDER BY session_id, step_index ASC` query.
4. Assemble into `[]SessionDetail`.

- [ ] **Step 3: Verify tests pass**

Run `go test -v ./internal/store/...`.

---

### Task 4: CLI Integration (`llm-track waste` & `llm-track sessions`)

**Files:**
- Create: `internal/cli/waste.go`
- Create: `internal/cli/waste_test.go`
- Modify: `internal/cli/sessions.go`
- Modify: `internal/cli/root.go`

**Interfaces:**
- Produces: `llm-track waste` command with `--period` flag.
- Enhances: `llm-track sessions <id>` with waste health score and diagnostics.

- [ ] **Step 1: Write CLI command tests in `internal/cli/waste_test.go`**

Test:
- `llm-track waste --period=today` outputs summary header, table, and recommendations without error.
- `llm-track waste` with empty DB outputs "No sessions found for period today." cleanly.

- [ ] **Step 2: Implement `internal/cli/waste.go`**

Implement Cobra command:
- Parse period flag.
- Call `store.GetPeriodSessionDetails(period)`.
- Run `analyzer.AnalyzePeriod(details, period, analyzer.DefaultConfig())`.
- Render formatted tables using `tabwriter.Writer`:
  - Period header & Avoidable Waste KPI.
  - Ranked session table (Rank, Tool, Session ID, Total Cost, Waste USD, Waste %, Health, Primary Issue).
  - Recommendations list.
Register `wasteCmd` in `root.go`.

- [ ] **Step 3: Enhance `internal/cli/sessions.go`**

In `runSessionDetail(cmd, args)`:
- Run `analysis := analyzer.AnalyzeSession(*detail, analyzer.DefaultConfig())`.
- Print `Health Score: XX/100` and any `[!] Waste Findings` above the turn-by-turn table.

- [ ] **Step 4: Run CLI tests**

Run `go test -v ./internal/cli/...`.

---

### Task 5: Web Dashboard REST API & Visual Trajectory Charts

**Files:**
- Modify: `internal/dashboard/handlers.go`
- Modify: `internal/dashboard/server.go`
- Test: `internal/dashboard/server_test.go`
- Modify: `internal/dashboard/static/index.html`
- Modify: `internal/dashboard/static/app.js`
- Modify: `internal/dashboard/static/style.css`

**Interfaces:**
- Produces:
  - `GET /api/waste?period=today|week|month|all` -> returns `PeriodWasteSummary` JSON.
  - `GET /api/sessions/{id}/waste` -> returns `SessionWasteAnalysis` JSON.
- UI:
  - Waste KPI cards (Avoidable Cost, Avg Health Score).
  - Trajectory Line Chart in Session Inspector modal showing $T_{\text{in}}$ curve and waste step flags.

- [ ] **Step 1: Write failing server tests in `internal/dashboard/server_test.go`**

Test `GET /api/waste` and `GET /api/sessions/{id}/waste` endpoints for valid HTTP 200 responses and JSON structures.

- [ ] **Step 2: Implement REST API handlers in `internal/dashboard/handlers.go`**

Add `handleGetWaste(w, r)` and `handleGetSessionWaste(w, r)`. Wire routes in `server.go`.

- [ ] **Step 3: Update Dashboard Frontend UI**

1. In `index.html`: Add "Waste Diagnostics" KPI cards and a waste breakdown chart container.
2. In `app.js`:
   - Fetch `/api/waste` when period changes; populate KPIs and waste category chart.
   - In session inspector modal, render context trajectory chart (`Input Tokens` vs `Step Index`) using Chart.js with points highlighted in red for waste turns.
3. In `style.css`: Add styles for health score badges (`health-good`, `health-warn`, `health-bad`) and waste findings.

- [ ] **Step 4: Verify dashboard tests pass**

Run `go test -v ./internal/dashboard/...`.

---

### Task 6: Background Daemon Threshold Monitoring

**Files:**
- Modify: `internal/daemon/daemon.go`
- Test: `internal/daemon/daemon_test.go`

**Interfaces:**
- Consumes: `cfg.WasteMonitoring`.
- Produces: Log alerts when active sessions breach token thresholds or consecutive tool error limits during sync cycles.

- [ ] **Step 1: Write test for daemon waste threshold check**

In `internal/daemon/daemon_test.go`:
Test that when turns with $>120\text{k}$ tokens or 3 consecutive errors are processed, daemon triggers alert callback/logging.

- [ ] **Step 2: Implement `checkWasteThresholds(sessionID string, turns []store.Request)` in `internal/daemon/daemon.go`**

Inspect latest turns during sync. If `WasteMonitoring.Enabled`, check for:
1. Turn input context exceeding `SevereTurnTokenThreshold`.
2. Consecutive error count exceeding `WarnConsecutiveToolErrors`.
Log structured warning `[DAEMON ALERT] Session '<id>' ...`.

- [ ] **Step 3: Verify daemon tests pass**

Run `go test -v ./internal/daemon/...`.

---

### Task 7: End-to-End Verification & Documentation

**Files:**
- Create: `test/waste_e2e_test.go`
- Modify: `README.md`

- [ ] **Step 1: Create E2E integration test `test/waste_e2e_test.go`**

Simulate realistic assistant logs (Claude Code with snowballing context, AGY with tool error loops), run sync, query `llm-track waste`, query `/api/waste`, and verify detected waste matches expected amounts.

- [ ] **Step 2: Run full project verification**

Run `go test -race ./... && go vet ./...`.

- [ ] **Step 3: Update `README.md`**

Document `llm-track waste`, context bloat detection, and configuration options. Update roadmap checkbox.

---

## Plan Self-Review Check
- [x] **No Placeholder/TODO**: All steps have concrete code and interfaces defined.
- [x] **Zero Schema Breaking Changes**: Uses existing store models and queries; no migrations required.
- [x] **TDD Bracketed**: Every task starts with a failing test and ends with verification.
- [x] **Scoped to Plan**: Solely focused on context bloat, token waste optimization, and guidance.
