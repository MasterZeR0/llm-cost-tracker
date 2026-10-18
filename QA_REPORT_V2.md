# Exhaustive Adversarial QA Audit Report (V2)

**Target System:** `llm-cost-tracker`  
**Scope:** Session Lifecycles & Turns, Prompt Cache Tracking, Agentic Tool Tracking, CLI Summary & Sessions Drilldown, Web Dashboard API & UI  
**Auditor:** Senior QA Engineer  
**Date:** October 18, 2026  
**Status:** Complete Audit with Actionable Findings  

---

## 1. Executive Summary

An exhaustive adversarial audit was conducted against the newly implemented analytics suite in `llm-cost-tracker`, covering:
1. **Session Lifecycles & Turn Management** (`llm-track sessions`, `GetSessionSummaries`, `GetSessionDetail`)
2. **Prompt Cache Analytics** ($T_{\text{hit}}$, $T_{\text{miss}}$, cache hit rate %, dollar savings)
3. **Agentic Tool Tracking** (tool calls, error rates, subagent invocations)
4. **Enhanced CLI Summary** (`llm-track summary`)
5. **Web Dashboard API & Frontend** (`/api/analytics/cache`, `/api/analytics/tools`, `/api/sessions`, `/api/sessions/detail`, static HTML/JS/CSS)
6. **Data Parsers & Persistence Pipeline** (AGY, Claude Code, SQLite WAL store, migration logic)

### Key Verdict
The core data ingestion and aggregation infrastructure is functional, but the audit revealed **3 High-Severity**, **4 Medium-Severity**, and **3 Low-Severity** issues spanning calculation accuracy, API contract violations, terminal presentation, and database indexing.

Most critically:
* **The prompt cache dollar savings formula is inaccurate across all non-fallback models** because the SQLite `pricing` table is never seeded, causing the store query to default to a static $0.95/MTok fallback for all models (undercounting Opus savings by up to 14x and overcounting Flash savings by up to 10x).
* **Missing entity lookups in the Dashboard API leak internal SQLite errors and return HTTP 500** instead of standard HTTP 404.
* **CLI terminal box formatting is severely misaligned** across both `summary` and `sessions` views due to mismatched hardcoded padding runes and unhandled variable-length model/tool names.
* **Database performance bottlenecks exist** due to missing indexes on `requests(session_id)` and `tool_calls(request_id)`.

---

## 2. Test Matrix & Execution Results

| Test ID | Component / Area | Input / Scenario | Expected Behavior | Actual Behavior | Status |
|---|---|---|---|---|---|
| **CLI-01** | `llm-track summary` | `--period=today`, `week`, `month`, `all` | Renders aligned box with stats | Rendered, but right border `│` misaligned by 3-6 columns | **FAIL (Alignment)** |
| **CLI-02** | `llm-track summary` | `--period=invalid` | Rejects with validation error | Returns code 1 with descriptive error | **PASS** |
| **CLI-03** | `llm-track sessions` | `--period=today`, `week`, `month`, `all` | Tabulates sessions cleanly | Renders, but table headers and borders mismatch by 2-8 columns | **FAIL (Alignment)** |
| **CLI-04** | `llm-track sessions` | `--limit=1` | Limits output to 1 row | Returns exactly 1 row | **PASS** |
| **CLI-05** | `llm-track sessions` | `--limit=0`, `--limit=-5` | Rejects or returns empty list | Silently resets limit to default 50 | **ANOMALY** |
| **CLI-06** | `llm-track sessions` | `<valid-full-id>`, `<valid-prefix-id>` | Shows turn-by-turn drilldown | Successfully renders session turns | **PASS** |
| **CLI-07** | `llm-track sessions` | `""` (empty string) | Return error or list recent | Dumps first random session via SQL `LIKE '%'` | **FAIL** |
| **CLI-08** | `llm-track sessions` | `"non-existent-id"` | User-friendly "not found" error | Leaks raw `sql: no rows in result set` | **FAIL** |
| **CLI-09** | CLI Duration Formatter | 0s, 45s, 12m, 13h, 48h | Formats seconds, mins, hours, days | Formats hours up to `48h+` (no days format `2d 0h`) | **ANOMALY** |
| **API-01** | `/api/analytics/cache` | Valid periods (`today`, `week`, `all`) | 200 OK with cache stats JSON | 200 OK with correct JSON shape | **PASS** |
| **API-02** | `/api/analytics/tools` | Valid periods | 200 OK with tool breakdown JSON | 200 OK with tool names & error rates | **PASS** |
| **API-03** | `/api/sessions` | `?limit=50&period=all` | 200 OK with array of summaries | 200 OK with array | **PASS** |
| **API-04** | `/api/sessions/detail` | `?id=<valid-id>` | 200 OK with summary + turns | 200 OK with detailed turns | **PASS** |
| **API-05** | `/api/sessions/detail` | `?id=nonexistent` | 404 Not Found JSON error | **HTTP 500 Internal Server Error** (`sql: no rows`) | **FAIL** |
| **API-06** | `/api/sessions/{id}` | Path parameter `/api/sessions/nonexistent` | 404 Not Found | **HTTP 500 Internal Server Error** | **FAIL** |
| **API-07** | `/api/sessions/detail` | `?id=` (empty) | 400 Bad Request | 400 Bad Request ("missing session id") | **PASS** |
| **API-08** | `/api/sessions/detail` | Wildcards `?id=%25`, `?id=_` | Exact or sanitized prefix match | Matches and returns first arbitrary DB record | **FAIL** |
| **WEB-01** | Dashboard UI (`index.html`) | DOM reference integrity | All element IDs match JS | All canvas & container IDs match | **PASS** |
| **WEB-02** | Frontend Script (`app.js`) | Error handling in `fetchJSON` | Handles non-OK HTTP gracefully | Throws raw `SyntaxError` on 500/404 plain text | **FAIL** |
| **WEB-03** | Frontend Script (`app.js`) | HTML escaping in tables | Sanitizes model/tool strings | Injects raw HTML; `<synthetic>` rendered invisible | **FAIL** |
| **WEB-04** | Frontend Modal | Escape key & backdrop click | Closes session drilldown modal | Both backdrop and Escape key close modal | **PASS** |
| **SYNC-01** | `llm-track sync` | Live directory scan | Ingests new records, stateful offset | Successfully ingests live records | **PASS** |
| **SYNC-02** | `llm-track sync` | Idempotency on unchanged files | Ingests 0 new records | Ingests 0 new records when files unchanged | **PASS** |
| **STORE-01** | SQLite Concurrency | 10 concurrent writers + 5 readers | WAL mode without busy lock errors | Completed 200 writes / 100 reads cleanly | **PASS** |
| **STORE-02** | Store Migration | Adding columns on existing database | Runs `ALTER TABLE` safely | Gracefully migrates schema | **PASS** |
| **STORE-03** | Foreign Key Cascades | Deleting requests cascades tool_calls | Cascades or restricts delete | `PRAGMA foreign_keys` not enabled; unenforced | **FAIL** |
| **STORE-04** | Query Performance | Joins on `tool_calls(request_id)` | Indexed lookup | Full table scan (missing index) | **FAIL** |

---

## 3. Discovered Defects & Vulnerabilities

### BUG-01 [HIGH]: Cache Dollar Savings Formula Uses Hardcoded Static Fallback (Pricing Table Never Populated)
* **Component:** `internal/store/queries.go` (`calculateCacheSavings`)
* **Severity:** High (Data Accuracy / Financial Metrics)
* **Steps to Reproduce:**
  1. Ingest requests with known models (e.g., Claude 3.5 Sonnet with 1,000,000 cache read tokens, Claude Opus with 1,000,000 cache read tokens).
  2. Query `st.GetCacheStats("all")` or view dashboard cache savings badge.
* **Expected Behavior:**
  * Claude Opus savings: $(5.00 - 0.50) = \$4.50/MTok ($4.50 savings).
  * Claude 3.5 Haiku savings: $(0.80 - 0.08) = \$0.72/MTok ($0.72 savings).
  * Total expected savings: ~$5.22.
* **Actual Behavior:**
  * Query returns **$1.90** savings.
* **Root Cause Analysis:**
  In `internal/store/queries.go:104-109`:
  ```sql
  SELECT coalesce(sum(r.cache_read_tokens * max(0.0000005, (coalesce(p.input_per_mtok, 1.25) - coalesce(p.cache_read_per_mtok, 0.30)) / 1000000.0)), 0.0)
  FROM requests r
  LEFT JOIN pricing p ON r.model = p.model_pattern
  ```
  1. The `pricing` table created in `schema.go` is **never populated** anywhere in the codebase. All pricing engine logic is held in-memory in Go (`pricing.Engine`).
  2. Therefore, `p.input_per_mtok` and `p.cache_read_per_mtok` are **always NULL**.
  3. `coalesce` defaults to `1.25 - 0.30 = $0.95/MTok` ($0.00000095/token) for every single model in existence.
  4. Furthermore, even if `pricing` had rows, `r.model = p.model_pattern` does an exact string equality check against glob patterns like `claude-3-5-sonnet*`, which would never match anyway without `GLOB` or wildcard handling.

---

### BUG-02 [HIGH]: Dashboard API Endpoints Return HTTP 500 and Leak Raw SQL Error on Missing Entities
* **Component:** `internal/dashboard/handlers.go` (`handleSessionDetail`)
* **Severity:** High (API Contract & Information Leakage)
* **Steps to Reproduce:**
  1. Send `GET /api/sessions/detail?id=does-not-exist` or `GET /api/sessions/does-not-exist`.
* **Expected Behavior:**
  * HTTP status `404 Not Found`.
  * Response body: `{"error": "session not found"}`.
* **Actual Behavior:**
  * HTTP status `500 Internal Server Error`.
  * Response body: `sql: no rows in result set\n` (Content-Type: `text/plain`).
* **Root Cause Analysis:**
  In `internal/dashboard/handlers.go:121-124`:
  ```go
  detail, err := s.store.GetSessionDetail(id)
  if err != nil {
      http.Error(w, err.Error(), http.StatusInternalServerError)
      return
  }
  ```
  `GetSessionDetail` returns `sql.ErrNoRows` when the ID does not match. The handler blindly returns `http.StatusInternalServerError` (500) and prints the raw driver error string. This also triggers a `SyntaxError` in `app.js` when `fetchJSON` tries to call `res.json()` on plain text.

---

### BUG-03 [HIGH]: SQL LIKE Wildcard & Empty Session ID Exploitation (`llm-track sessions ""` & `%`)
* **Component:** `internal/store/queries.go` (`GetSessionDetail`) & `internal/cli/sessions.go`
* **Severity:** High (Data Leakage & Input Validation)
* **Steps to Reproduce:**
  1. Run `llm-track sessions ""` in the terminal.
  2. Run `llm-track sessions "%"` or `curl "/api/sessions/detail?id=%25"`.
* **Expected Behavior:**
  * `llm-track sessions ""` should either display the sessions list or return an error `session ID cannot be empty`.
  * Queries with `%` or `_` should escape wildcards or reject invalid identifiers.
* **Actual Behavior:**
  * `llm-track sessions ""` dumps the full turn details of a random first session in the database!
  * `llm-track sessions "%"` similarly dumps the first session.
* **Root Cause Analysis:**
  In `internal/store/queries.go:397-401`:
  ```sql
  SELECT session_id FROM requests
  WHERE session_id = ? OR session_id LIKE ? || '%'
  LIMIT 1
  ```
  When `sessionID == ""`, `session_id LIKE '' || '%'` becomes `session_id LIKE '%'`. In SQL, `'%'` matches any string of any length. With `LIMIT 1`, SQLite simply scans the table and returns the first row encountered.

---

### BUG-04 [MEDIUM]: Token Accounting Discrepancy — `TotalTokens` Excludes `CacheReadTokens`
* **Component:** `internal/parsers/agy.go`, `internal/parsers/claude_code.go`, `internal/cli/sessions.go`
* **Severity:** Medium (UI Consistency & User Confusion)
* **Steps to Reproduce:**
  1. Inspect any session with cache hits (e.g. `llm-track sessions 77e9ca17`).
* **Expected Behavior:**
  * If a session processed 2,801,951 cache hits and 218,304 cache misses, total tokens processed should be at least $2,801,951 + 218,304 + \text{OutputTokens} = 3,046,496$.
* **Actual Behavior:**
  * CLI output displays:
    `Tokens: 244545 (Cache: 2801951 Hit, 218304 Miss, 92.8%)`
* **Root Cause Analysis:**
  In `internal/parsers/agy.go:205`:
  ```go
  TotalTokens: inTokens + outTokens,
  ```
  And in `internal/parsers/claude_code.go:163`:
  ```go
  TotalTokens: evt.Message.Usage.InputTokens + evt.Message.Usage.OutputTokens,
  ```
  Both parsers calculate `TotalTokens` strictly as `input_tokens + output_tokens`, entirely omitting `cache_read_tokens`. As a result, the database column `total_tokens` stores only non-cached tokens. When displayed alongside `Cache: X Hit`, `TotalTokens` appears 10x–20x smaller than the cache hits alone.

---

### BUG-05 [MEDIUM]: Broken Terminal Box Alignments & Column Overflow in CLI
* **Component:** `internal/cli/summary.go`, `internal/cli/sessions.go`
* **Severity:** Medium (CLI Polish & UX)
* **Steps to Reproduce:**
  1. Run `llm-track summary --period=all`.
  2. Run `llm-track sessions --period=all`.
* **Expected Behavior:**
  * The right border `│` should form a straight vertical column down the right-hand edge.
* **Actual Behavior:**
  * In `summary`:
    * Outer box width is 63 runes.
    * "By Tool" lines are formatted with width 57 runes (`short by 6 characters`).
    * "By Model" lines are formatted with width 60 runes (`short by 3 characters`).
    * Model names longer than 16 characters (e.g. `claude-opus-4-6-thinking` = 24 chars) blow out the width to 66 runes.
    * Tool names longer than 18 characters (e.g. `replace_file_content` = 20 chars) blow out the line to 64 runes.
  * In `sessions`:
    * Outer box width is 95 runes.
    * Header `│ Recent Sessions — all │` is padded to only 87 runes (`short by 8 characters`).
    * Table column header `│ SESSION ID TOOL ... │` is 97 runes (`2 runes wider than top border`).
    * Session detail overview line `│ Tool: ... | Turns: ... │` is 100 runes (`5 runes wider than top border`).
* **Root Cause Analysis:**
  Format strings in `summary.go` and `sessions.go` were assembled with manual space padding that does not sum to the box boundary length, and variable strings are not truncated or dynamically padded to fixed widths.

---

### BUG-06 [MEDIUM]: Missing SQLite Indexes on `requests(session_id)` and `tool_calls(request_id)` & Disabled Foreign Keys
* **Component:** `internal/store/schema.go`, `internal/store/store.go`
* **Severity:** Medium (Database Performance & Referential Integrity)
* **Root Cause Analysis:**
  1. `schema.go` defines:
     ```sql
     CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp);
     CREATE INDEX IF NOT EXISTS idx_requests_tool ON requests(tool);
     CREATE INDEX IF NOT EXISTS idx_requests_model ON requests(model);
     CREATE INDEX IF NOT EXISTS idx_requests_tool_timestamp ON requests(tool, timestamp);
     ```
     **Missing Index:** No index on `requests(session_id)`. Every session query (`GetSessionDetail`, `GetSessionSummaries`) performs full table scans on `requests`.
  2. `tool_calls` defines indexes on `session_id`, `tool_name`, `status`, but **no index on `request_id`**.
     In `GetSessionDetail:516`:
     ```sql
     SELECT ... FROM tool_calls WHERE request_id = ? ORDER BY created_at ASC
     ```
     And in `GetToolCallStats:557`:
     ```sql
     JOIN requests r ON tc.request_id = r.id
     ```
     These queries perform full table scans across `tool_calls` for every turn drilldown.
  3. Neither `PRAGMA foreign_keys = ON;` nor connection URI parameter `_foreign_keys=1` is configured. Foreign key constraints and `ON DELETE CASCADE` on `tool_calls` are completely ignored by SQLite.

---

### BUG-07 [MEDIUM]: Frontend HTML Injection & Unescaped DOM Insertion in Dashboard
* **Component:** `internal/dashboard/static/app.js` (`loadSessions`, `showSessionDetail`)
* **Severity:** Medium (Security / Presentation Bug)
* **Root Cause Analysis:**
  1. In `app.js:203`:
     ```javascript
     onclick="showSessionDetail('${encodeURIComponent(s.session_id)}')"
     ```
     `encodeURIComponent` does **not** escape single quotes (`'`). If a session ID contains a single quote, the onclick attribute breaks and permits inline script execution.
  2. In `app.js:303`:
     ```javascript
     <td>${t.model || '—'}</td>
     ```
     `t.model` is injected directly into `innerHTML` without HTML entity encoding. For models like `<synthetic>` or `<unknown>` (which appear in test and proxy logs), the browser interprets them as unknown HTML tags (`<synthetic></synthetic>`), rendering the cell completely blank.

---

### BUG-08 [LOW]: Missing Day Duration Formatting & Sub-Second Granularity
* **Component:** `internal/cli/sessions.go` (`formatDuration`), `internal/dashboard/static/app.js` (`formatDuration`)
* **Severity:** Low (Feature Completeness)
* **Observation:**
  Both Go and JavaScript format durations up to hours only (`48h 12m` or `120h`). Neither formats durations in days (`2d 0h` or `5d 0h`). Sub-second durations (e.g. 400ms) are truncated to `0s` because `int(d.Seconds())` rounds down.

---

### BUG-09 [LOW]: Step Index in CLI Session Detail Shows `0` for Unindexed Turns
* **Component:** `internal/cli/sessions.go:156`
* **Severity:** Low (CLI Display)
* **Observation:**
  In `app.js`, step numbering falls back: `const stepNum = t.step_index > 0 ? t.step_index : (idx + 1);`.
  In `sessions.go`, line 156 prints `turn.StepIndex` directly. For all sessions synced prior to the step index column addition or where logs omit step index, the STEP column renders as `0` for every turn in the session.

---

### BUG-10 [LOW]: Silent Fallback on `--limit=0` and Negative Limits to Default 50
* **Component:** `internal/store/queries.go:305` (`GetSessionSummaries`)
* **Severity:** Low (Parameter Handling)
* **Observation:**
  Passing `--limit=0` or `--limit=-5` does not return 0 records or report a validation error; instead `if limit <= 0 { limit = 50 }` resets the limit to 50 records.

---

## 4. Root Cause Analysis Summary & Code Links

```
┌──────────────────────────────────────┬─────────────────────────────────────────────────┬────────────────────────────────────────────────────────┐
│ Defect                               │ Code Location                                   │ Root Cause                                             │
├──────────────────────────────────────┼─────────────────────────────────────────────────┼────────────────────────────────────────────────────────┤
│ Inaccurate Cache Savings             │ internal/store/queries.go:104-109               │ SQLite pricing table is empty; LEFT JOIN always null   │
│ HTTP 500 on Missing Session          │ internal/dashboard/handlers.go:121-124          │ sql.ErrNoRows not mapped to 404 Not Found              │
│ Wildcard / Empty Session Match       │ internal/store/queries.go:397-401               │ LIKE '' || '%' matches all rows                        │
│ Cache Tokens Excluded from Total     │ internal/parsers/agy.go:205                     │ TotalTokens = Input + Output (excludes CacheRead)      │
│ Jagged Box Borders in CLI            │ internal/cli/summary.go, sessions.go            │ Hardcoded format string padding mismatches box width   │
│ Missing Database Indexes             │ internal/store/schema.go:66-70                  │ requests(session_id) & tool_calls(request_id) unindexed│
│ Unescaped HTML & Single Quote in UI  │ internal/dashboard/static/app.js:203, 303       │ Raw innerHTML assignment without escaping              │
└──────────────────────────────────────┴─────────────────────────────────────────────────┴────────────────────────────────────────────────────────┘
```

---

## 5. Remediation Plan & Recommendations for the Fullstack Engineer

### 1. Fix Cache Savings Calculation
* **Recommended approach:**
  Seed the SQLite `pricing` table on startup from `pricing.DefaultModelPrices`, OR compute savings in Go by fetching model token totals and delegating to `pricing.Engine.CalculateCost()`.
  If doing in SQL, populate the `pricing` table during `store.NewStore()` so model rates exist, and normalize model patterns or use SQL `LIKE` / prefix matching.

### 2. Fix Dashboard API HTTP Status Codes
* In `handleSessionDetail`:
  ```go
  detail, err := s.store.GetSessionDetail(id)
  if err != nil {
      if errors.Is(err, sql.ErrNoRows) {
          writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
          return
      }
      http.Error(w, err.Error(), http.StatusInternalServerError)
      return
  }
  ```
* In `fetchJSON` (`app.js`), check `if (!res.ok)` and throw an error with the parsed JSON message or status code rather than calling `res.json()` directly.

### 3. Sanitize Session ID Inputs
* In `GetSessionDetail`:
  ```go
  sessionID = strings.TrimSpace(sessionID)
  if sessionID == "" {
      return nil, sql.ErrNoRows
  }
  // Escape LIKE special characters if using LIKE:
  escapedID = strings.ReplaceAll(sessionID, "%", "\\%")
  escapedID = strings.ReplaceAll(escapedID, "_", "\\_")
  ```

### 4. Reconcile Total Tokens Accounting
* Update parsers to compute `TotalTokens` as:
  ```go
  TotalTokens: inTokens + outTokens + cacheTokens,
  ```
  Or clearly label prompt tokens in UI and CLI as:
  `Prompt Tokens (Miss + Hit)` vs `Output Tokens`.

### 5. Fix CLI Terminal Box Alignments
* Define a single constant width for the summary box (e.g., 63 runes) and session box (e.g., 95 runes).
* Truncate tool and model names with an ellipsis if they exceed column width.
* Ensure all format specifier widths and static padding strings sum precisely to the box width.

### 6. Add Missing Database Indexes and Enable Foreign Keys
* Add to `schema.go`:
  ```sql
  CREATE INDEX IF NOT EXISTS idx_requests_session ON requests(session_id);
  CREATE INDEX IF NOT EXISTS idx_tool_calls_request ON tool_calls(request_id);
  ```
* Add `_foreign_keys=1` to SQLite connection string in `store.go`:
  ```go
  db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_busy_timeout=5000&_foreign_keys=1")
  ```

### 7. Sanitize HTML in Frontend UI
* Create an `escapeHTML(str)` utility in `app.js` and use it on `t.model`, `s.tool`, `s.session_id`.
* For action buttons, attach event listeners with `element.addEventListener('click', ...)` or `data-session-id="..."` instead of inline string interpolation in `onclick`.
