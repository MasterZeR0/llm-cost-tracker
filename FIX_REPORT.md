# LLM Cost Tracker (`llm-track`) QA Remediation & Fix Report

**Date:** October 17, 2026  
**Status:** All Issues Resolved & Verified  
**Target Environment:** macOS (Darwin 24.3.0), Go 1.24 / SQLite 3  
**Verified Binary:** `/Users/nehaverma/orca/projects/llm-cost-tracker/bin/llm-track`  

---

## 1. Executive Summary

Following the comprehensive QA findings documented in `QA_REPORT.md`, all identified bugs, edge-case failure modes, and UX flaws have been remediated, verified against reproducible test scenarios, and covered with automated regression tests.

All tests across all packages pass with `-race -count=1` enabled:
```bash
go test -race -count=1 ./...
# Result: PASS (internal/cli, internal/config, internal/daemon, internal/dashboard, internal/parsers, internal/pricing, internal/store, test)
```

---

## 2. Detailed Bug Fixes & Architectural Enhancements

### 2.1 [CRITICAL] BUG-01: Real Codex SQLite `created_at` Type Scan Failure
- **File:** `internal/parsers/codex.go`, `internal/parsers/codex_test.go`
- **Root Cause:** Official Codex state databases (`~/.codex/state_5.sqlite`) store `created_at` as `INTEGER` (unix timestamp in milliseconds or seconds), whereas earlier mock tests used `DATETIME` strings. Scanning `int64` into `sql.NullTime` failed silently with driver scan errors, causing 0 threads to be imported. Furthermore, querying `WHERE created_at > ?` with a string parameter against an integer column in SQLite evaluated `INTEGER > TEXT` as false due to SQLite storage class affinity rules.
- **Solution:**
  - Added schema introspection (`SELECT created_at FROM threads ...` / `PRAGMA table_info(threads)`) to detect whether `created_at` is integer (seconds vs milliseconds) or text/datetime.
  - Implemented `parseCodexTimestamp(val any) (time.Time, bool)` handling `int64` (> 1e11 treated as epoch milliseconds, <= 1e11 as epoch seconds), `time.Time`, `string`, and `[]byte`.
  - Dynamically bound incremental query parameters (`state.LastModified.UnixMilli()`, `state.LastModified.Unix()`, or `state.LastModified`) to match the column type, preserving query correctness and index utilization.
  - Added sub-second offsets per turn to prevent duplicate-second collisions.
- **Tests Added:**
  - `TestCodexParser_IntegerEpochSeconds`: Verifies ingestion and incremental query against unix epoch seconds.
  - `TestCodexParser_IntegerEpochMilliseconds`: Verifies ingestion and incremental query against unix epoch milliseconds.

---

### 2.2 [HIGH] BUG-02: Recursive `fsnotify` Directory Watching
- **File:** `internal/daemon/daemon.go`, `internal/daemon/daemon_test.go`
- **Root Cause:** `fsnotify` on macOS and Linux is non-recursive. Watching tool root paths like `~/.claude/projects` or `~/.gemini/antigravity-cli/brain` missed events in nested directories such as `projects/<project>/<session>.jsonl` or `brain/<id>/.system_generated/logs/transcript.jsonl`.
- **Solution:**
  - Implemented `watchRecursive(watcher *fsnotify.Watcher, root string) error`, which uses `filepath.Walk` to add watches to all existing nested subdirectories.
  - Updated the event loop to detect newly created directories (`event.Has(fsnotify.Create)` on a directory) and dynamically add them and their sub-trees to the watcher.
- **Tests Added:**
  - `TestDaemon_RecursiveWatch`: Spawns daemon with 10-minute poll interval, writes events to nested subdirectories, and confirms immediate ingestion via `fsnotify` within <350ms.

---

### 2.3 [HIGH] BUG-03: Duplicate PID / Daemon Instance Detection
- **File:** `internal/daemon/daemon.go`, `internal/cli/daemon.go`, `internal/daemon/daemon_test.go`
- **Root Cause:** `daemon start` previously overwrote `daemon.pid` unconditionally without checking if an existing daemon process was already active.
- **Solution:**
  - Implemented `(d *Daemon) CheckRunningPID() (int, error)`. If `daemon.pid` exists, reads the PID and tests liveness via `syscall.Kill(pid, 0)`.
  - If the process is alive (or returns `EPERM`), returns error: `daemon is already running (PID %d)`.
  - If the process is dead (`ESRCH`), removes the stale PID file and allows startup to proceed cleanly.
  - Added early check in `cli.daemonStartCmd` and in `d.Start(ctx)`.
- **Tests Added:**
  - `TestDaemon_DuplicatePIDDetection`: Confirms running daemon prevents duplicate instance startup.

---

### 2.4 [HIGH] BUG-04: Pricing Engine Wildcards on Provider-Prefixed Models
- **File:** `internal/pricing/engine.go`, `internal/pricing/engine_test.go`
- **Root Cause:** Models with provider prefixes (e.g. `anthropic/claude-3-5-sonnet-20241022`, `openai/gpt-4o`, `google/gemini-2.0-flash-exp`) failed `filepath.Match("claude-3-5-sonnet*", ...)` because `filepath.Match` wildcards do not match `/` path separators.
- **Solution:**
  - Implemented `getModelCandidates(modelLower string) []string` which produces candidate matches by stripping known provider prefixes (`anthropic/`, `openai/`, `google/`, `openrouter/`, `bedrock/`, `azure/`, etc.) and by taking the model base name after `/`.
  - Supported pattern matching both with and without provider prefixes, preserving pattern specificity ordering (e.g., `gpt-4o-mini` matches before `gpt-4o`).
- **Tests Added:**
  - `TestPricingEngine_ProviderPrefixedModels`: Confirms correct pricing for `anthropic/claude-3-5-sonnet-20241022`, `openai/gpt-4o`, `openai/gpt-4o-mini`, `google/gemini-2.0-flash-exp`, and `openrouter/anthropic/claude-3-5-sonnet`.

---

### 2.5 [MEDIUM] BUG-05: Stale PID File Cleanup in `daemon stop`
- **File:** `internal/daemon/daemon.go`, `internal/cli/daemon.go`, `internal/daemon/daemon_test.go`
- **Root Cause:** When `daemon stop` was executed on a dead process, `proc.Signal` returned `os: process already finished` or `ESRCH`, exiting with an error and leaving `daemon.pid` abandoned on disk.
- **Solution:**
  - `StopDaemon` checks liveness via `syscall.Kill(pid, 0)` and catches `os.ErrProcessDone`, `syscall.ESRCH`, and "process already finished" errors.
  - On dead process detection, removes the stale PID file and returns a descriptive error/status: `daemon process (PID %d) is not running; removed stale PID file`.
  - In `cli.daemonStopCmd`, prints the stale cleanup notice cleanly and exits with code 0.
- **Tests Added:**
  - `TestDaemon_StopDaemonStalePID`: Verifies that `daemon stop` removes the stale PID file when the process is dead.

---

### 2.6 [MEDIUM] BUG-06: Second-Precision Timestamp Collision & Deduplication
- **File:** `internal/store/schema.go`, `internal/store/store.go`, `internal/store/queries.go`, `internal/parsers/agy.go`, `internal/parsers/claude_code.go`
- **Root Cause:** `requests` table enforces `UNIQUE(tool, session_id, timestamp, model)`. When tools (such as AGY) emit events with second-resolution ISO-8601 timestamps, subsequent events within the same second had identical formatted timestamps and were dropped by `INSERT OR IGNORE`.
- **Solution:**
  - Changed `store.InsertRequests` to format timestamps with `time.RFC3339Nano` (preserving sub-second precision).
  - Updated store queries (`GetRecentRequests`) to parse `time.RFC3339Nano` with fallback to `time.RFC3339`.
  - In `AGYParser`, leveraged `step_index` or added sequential sub-second millisecond offsets for same-timestamp events.
  - In `ClaudeCodeParser`, added microsecond sequence offsets when identical timestamps collide within a session.
  - Replays of identical files produce identical sub-second timestamps, guaranteeing that real duplicates are properly ignored while valid distinct turns are preserved.
- **Tests Added:**
  - `TestAGYParser_SameSecondDeduplication`: Tests multiple events at `2026-10-17T11:51:18Z`, verifying 2 records inserted on first sync and 0 records on duplicate replay.

---

### 2.7 [LOW/UX] FLAW-07 through FLAW-10 Polish
1. **FLAW-07: Claude Code Billing/Quota Error Handling** (`internal/parsers/claude_code.go`):
   - Checked `evt.Error` and `evt.IsApiErrorMessage`.
   - When present (e.g. `billing_error`), records request status as `"error"` rather than `"success"`.
   - Verified via `TestClaudeCodeParser_BillingErrorStatus`.
2. **FLAW-08: CLI `--period` Validation** (`internal/cli/summary.go`):
   - Added validation against allowed values (`today`, `week`, `month`, `all`) in `summaryCmd.PreRunE` and `RenderSummary`.
   - Returns clear error: `invalid period "<val>": must be one of today, week, month, all`.
   - Verified via `TestRenderSummary_InvalidPeriod`.
3. **FLAW-09: Dashboard Recent Requests Period Filtering** (`internal/store/queries.go`, `internal/dashboard/handlers.go`, `internal/dashboard/static/app.js`):
   - Updated `GetRecentRequests(limit int, periodOpt ...string)` to apply `periodClause(period)`.
   - Updated `/api/requests` endpoint to extract `?period=` and pass it to store.
   - Updated `app.js` to pass `?period=${period}` in `loadRequests(period)`.
   - Verified via `TestDashboardAPI_AllEndpoints` and `TestStore_Queries`.
4. **FLAW-10: Config `disable_other_tools` Support** (`internal/config/config.go`, `internal/config/config_test.go`):
   - Added `disable_other_tools: true` flag in config YAML.
   - When set, automatically disables default tools not explicitly defined in the user's config file.
   - Verified via `TestLoadConfig_DisableOtherTools`.

---

## 3. Verification Matrix

| Issue ID | Severity | Verification Status | Verification Method |
| :--- | :--- | :--- | :--- |
| **BUG-01** | Critical | **PASS** | Unit tests + Live CLI reproduction with SQLite `INTEGER created_at` |
| **BUG-02** | High | **PASS** | Unit test `TestDaemon_RecursiveWatch` (nested directory detection in <350ms) |
| **BUG-03** | High | **PASS** | Unit test + Live CLI duplicate start prevention |
| **BUG-04** | High | **PASS** | Unit tests + Live CLI summary cost calculation with `openai/gpt-4o` |
| **BUG-05** | Medium | **PASS** | Unit test + Live CLI stale PID file cleanup on `daemon stop` |
| **BUG-06** | Medium | **PASS** | Unit test `TestAGYParser_SameSecondDeduplication` + Live CLI sync replay |
| **FLAW-07** | Low/UX | **PASS** | Unit test `TestClaudeCodeParser_BillingErrorStatus` + Live DB inspection |
| **FLAW-08** | Low/UX | **PASS** | Unit test + CLI validation error output |
| **FLAW-09** | Low/UX | **PASS** | Unit test + `/api/requests?period=today` endpoint test |
| **FLAW-10** | Low/UX | **PASS** | Unit test `TestLoadConfig_DisableOtherTools` |

---

## 4. Conclusion

All reported issues have been fixed and verified. The codebase builds cleanly and passes all race-detector tests. The compiled binary `/Users/nehaverma/orca/projects/llm-cost-tracker/bin/llm-track` is ready for production use.
