# IDE Assistant Adapters (Phase 1) Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Implement 5 dedicated adapters (Cline, Roo Code, Cursor, Windsurf, GitHub Copilot CLI) with shared SQLite `vscdb` reader and token estimation utilities to track costs, tokens, latencies, and tool observability.

**Architecture:** Shared helper utilities (`vscdb` reader for SQLite immutable access, `tokenestimator` for closed-proxy token approximation, and OS-aware path expansion) underpin five standalone parser modules implementing the `parsers.Parser` interface with deterministic deduplication and incremental sync.

**Tech Stack:** Go 1.22+, SQLite (`mattn/go-sqlite3`), JSON / JSONL, Go standard library.

**Spec:** [`docs/superpowers/specs/2026-10-18-ide-assistant-adapters-design.md`](file:///Users/nehaverma/orca/projects/llm-cost-tracker/docs/superpowers/specs/2026-10-18-ide-assistant-adapters-design.md)

## Global Constraints
- Target platform: macOS, Linux, and Windows cross-platform compatibility.
- Non-invasive: all storage and SQLite databases must be opened read-only with immutable flags (`?mode=ro&immutable=1`) without locking editor processes.
- Idempotent and incremental: all parsed turns must generate deterministic request IDs (`fmt.Sprintf("%s:%s:%d", tool, sessionID, stepIndex)`).
- Status marking: requests with native tokens use `Status: "success"`; requests with heuristically estimated tokens use `Status: "estimated"`.
- Registry pattern: each new parser registers itself via `parsers.Register()` and is available in `parsers.All()`.

## Review Focus
1. Truncated or rotated log file resets `LastOffset` safely to 0 without panic or silent failure.
2. Locked `state.vscdb` during active Cursor/Windsurf sessions opened cleanly via read-only immutable pragma without throwing `database is locked`.
3. In-flight tasks with newly streaming turns update turn output/costs without duplicating rows in `store.Store`.
4. Prompt cache hit and write tokens accurately attributed to `CacheReadTokens` and `CacheWriteTokens` for Claude models in Cline and Roo Code.
5. Unknown proprietary models (e.g. `cursor-small`, `cascade-base`) cleanly default to $0.00 cost without causing calculation errors or nil pointer exceptions.

---

### Task 1: OS-Aware Path Discovery & Default Configurations

**Files:**
- Create: `internal/config/paths.go`
- Test: `internal/config/paths_test.go`
- Modify: `internal/config/config.go:62-93`

**Interfaces:**
- Consumes: `os.UserHomeDir()`, `runtime.GOOS`, environment variables (`APPDATA`, `XDG_CONFIG_HOME`).
- Produces: `config.ResolveAppSupportDirs() []string`, `config.DefaultPathsForTool(tool string) []string`.

- [ ] **Step 1: Write the failing test**

```go
// internal/config/paths_test.go
package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsForTool(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home: %v", err)
	}

	tests := []struct {
		tool     string
		contains string
	}{
		{"cline", "saoudrizwan.claude-dev"},
		{"roo_code", "rooveterinaryinc.roo-cline"},
		{"cursor", "Cursor"},
		{"windsurf", "Windsurf"},
		{"copilot_cli", "copilot"},
	}

	for _, tc := range tests {
		paths := DefaultPathsForTool(tc.tool)
		if len(paths) == 0 {
			t.Errorf("expected paths for tool %s, got none", tc.tool)
			continue
		}
		found := false
		for _, p := range paths {
			if filepath.IsAbs(p) && len(p) > len(home) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %s paths did not resolve to absolute home path: %v", tc.tool, paths)
		}
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/config -run TestDefaultPathsForTool`  
Expected: FAIL with `undefined: DefaultPathsForTool`.

- [ ] **Step 3: Implement OS-aware path resolver and update `DefaultConfig()`**

```go
// internal/config/paths.go
package config

import (
	"os"
	"path/filepath"
	"runtime"
)

func ResolveAppSupportDirs(appName string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	var dirs []string
	switch runtime.GOOS {
	case "darwin":
		dirs = append(dirs, filepath.Join(home, "Library", "Application Support", appName))
	case "windows":
		if appdata := os.Getenv("APPDATA"); appdata != "" {
			dirs = append(dirs, filepath.Join(appdata, appName))
		}
		dirs = append(dirs, filepath.Join(home, "AppData", "Roaming", appName))
	default: // linux and others
		if xdg := os.Getenv("XDG_CONFIG_HOME"); xdg != "" {
			dirs = append(dirs, filepath.Join(xdg, appName))
		}
		dirs = append(dirs, filepath.Join(home, ".config", appName))
	}
	return dirs
}

func DefaultPathsForTool(tool string) []string {
	home, err := os.UserHomeDir()
	if err != nil {
		return nil
	}

	switch tool {
	case "cline":
		var paths []string
		for _, codeDir := range ResolveAppSupportDirs("Code") {
			paths = append(paths, filepath.Join(codeDir, "User", "globalStorage", "saoudrizwan.claude-dev", "tasks"))
		}
		return paths
	case "roo_code":
		var paths []string
		for _, codeDir := range ResolveAppSupportDirs("Code") {
			paths = append(paths, filepath.Join(codeDir, "User", "globalStorage", "rooveterinaryinc.roo-cline", "tasks"))
		}
		return paths
	case "cursor":
		var paths []string
		for _, cursorDir := range ResolveAppSupportDirs("Cursor") {
			paths = append(paths, filepath.Join(cursorDir, "User", "workspaceStorage"))
			paths = append(paths, filepath.Join(cursorDir, "User", "globalStorage"))
		}
		return paths
	case "windsurf":
		var paths []string
		for _, windsurfDir := range ResolveAppSupportDirs("Windsurf") {
			paths = append(paths, filepath.Join(windsurfDir, "User", "workspaceStorage"))
		}
		paths = append(paths, filepath.Join(home, ".codeium", "windsurf"))
		return paths
	case "copilot_cli":
		return []string{
			filepath.Join(home, ".copilot-cli"),
			filepath.Join(home, ".config", "gh", "copilot"),
		}
	default:
		return nil
	}
}
```

Update `DefaultConfig()` in `internal/config/config.go` to add default configurations for `cline`, `roo_code`, `cursor`, `windsurf`, and `copilot_cli` using `DefaultPathsForTool()`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/config`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/config/paths.go internal/config/paths_test.go internal/config/config.go
git commit -m "feat(config): add OS-aware path resolution for IDE assistants"
```

---

### Task 2: Shared Token Estimator Helper

**Files:**
- Create: `internal/parsers/tokenestimator/estimator.go`
- Test: `internal/parsers/tokenestimator/estimator_test.go`

**Interfaces:**
- Produces: `tokenestimator.EstimateTokens(promptText, responseText string) (inputTokens, outputTokens int64)`.

- [ ] **Step 1: Write the failing test**

```go
// internal/parsers/tokenestimator/estimator_test.go
package tokenestimator

import "testing"

func TestEstimateTokens(t *testing.T) {
	prompt := "Write a fast go function to calculate fibonacci numbers."
	response := "Here is a clean implementation of Fibonacci in Go:\n\nfunc Fib(n int) int {\n    if n <= 1 { return n }\n    return Fib(n-1) + Fib(n-2)\n}"

	inTokens, outTokens := EstimateTokens(prompt, response)
	if inTokens <= 0 {
		t.Errorf("expected positive input tokens, got %d", inTokens)
	}
	if outTokens <= 0 {
		t.Errorf("expected positive output tokens, got %d", outTokens)
	}
	if inTokens >= int64(len(prompt)) {
		t.Errorf("input tokens should be smaller than char length: tokens=%d, chars=%d", inTokens, len(prompt))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers/tokenestimator`  
Expected: FAIL with `undefined: EstimateTokens`.

- [ ] **Step 3: Write implementation**

```go
// internal/parsers/tokenestimator/estimator.go
package tokenestimator

import (
	"math"
	"strings"
)

// EstimateTokens calculates approximate token counts for prompt and completion
// based on standard subword token ratios (~4.0 chars/token for prompt, ~3.8 for completion).
func EstimateTokens(promptText, responseText string) (int64, int64) {
	promptLen := len(strings.TrimSpace(promptText))
	respLen := len(strings.TrimSpace(responseText))

	var inputTokens int64
	if promptLen > 0 {
		inputTokens = int64(math.Ceil(float64(promptLen) / 4.0))
		if inputTokens < 1 {
			inputTokens = 1
		}
	}

	var outputTokens int64
	if respLen > 0 {
		outputTokens = int64(math.Ceil(float64(respLen) / 3.8))
		if outputTokens < 1 {
			outputTokens = 1
		}
	}

	return inputTokens, outputTokens
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers/tokenestimator`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/tokenestimator/
git commit -m "feat(parsers): implement shared token estimation heuristic helper"
```

---

### Task 3: Shared `vscdb` SQLite Reader Helper

**Files:**
- Create: `internal/parsers/vscdb/vscdb.go`
- Test: `internal/parsers/vscdb/vscdb_test.go`

**Interfaces:**
- Produces:
  ```go
  type Reader struct { db *sql.DB }
  func Open(path string) (*Reader, error)
  func (r *Reader) GetKey(key string) ([]byte, error)
  func (r *Reader) GetKeysLike(prefix string) (map[string][]byte, error)
  func (r *Reader) Close() error
  ```

- [ ] **Step 1: Write the failing test**

```go
// internal/parsers/vscdb/vscdb_test.go
package vscdb

import (
	"database/sql"
	"os"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func createMockVSCDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', '{"composers": []}');
		INSERT INTO ItemTable (key, value) VALUES ('workbench.panel.aichat.chatdata', '{"chats": []}');
	`)
	if err != nil {
		t.Fatalf("failed to populate mock vscdb: %v", err)
	}
	return dbPath
}

func TestVSCDBReader(t *testing.T) {
	dbPath := createMockVSCDB(t)

	reader, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer reader.Close()

	val, err := reader.GetKey("composer.composerData")
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}
	if string(val) != `{"composers": []}` {
		t.Errorf("unexpected value: %s", string(val))
	}

	keys, err := reader.GetKeysLike("workbench.%")
	if err != nil {
		t.Fatalf("GetKeysLike failed: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers/vscdb`  
Expected: FAIL with `undefined: Open`.

- [ ] **Step 3: Implement `vscdb.Reader`**

```go
// internal/parsers/vscdb/vscdb.go
package vscdb

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

type Reader struct {
	db *sql.DB
}

func Open(path string) (*Reader, error) {
	// Open in immutable read-only mode so editor processes are not locked.
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", url.PathEscape(path))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open vscdb: %w", err)
	}

	// Verify ItemTable exists
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='ItemTable'").Scan(&name)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("ItemTable not found in %s: %w", path, err)
	}

	return &Reader{db: db}, nil
}

func (r *Reader) GetKey(key string) ([]byte, error) {
	var val []byte
	err := r.db.QueryRow("SELECT value FROM ItemTable WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (r *Reader) GetKeysLike(pattern string) (map[string][]byte, error) {
	rows, err := r.db.Query("SELECT key, value FROM ItemTable WHERE key LIKE ?", pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[string][]byte)
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		results[k] = v
	}
	return results, nil
}

func (r *Reader) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}
```

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers/vscdb`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/vscdb/
git commit -m "feat(parsers): implement shared vscdb SQLite reader helper"
```

---

### Task 4: Cline Parser (`cline`)

**Files:**
- Create: `internal/parsers/cline.go`
- Test: `internal/parsers/cline_test.go`
- Testdata: `internal/parsers/testdata/cline/tasks/task-001/api_conversation_history.json`

**Interfaces:**
- Implements: `parsers.Parser` (`Name() string`, `Discover(cfg) ([]DataSource, error)`, `Parse(source, state) ([]store.Request, store.SyncState, error)`).
- Produces: `store.Request` with tool observability and cache read/write tokens.

- [ ] **Step 1: Write test fixture and failing test**

Create `internal/parsers/testdata/cline/tasks/task-001/api_conversation_history.json` with sample Anthropic format turns containing:
- Model: `claude-3-5-sonnet-20241022`
- Usage: `input_tokens: 1200`, `output_tokens: 350`, `cache_creation_input_tokens: 5000`, `cache_read_input_tokens: 2500`
- Tool blocks: `<execute_command>ls -la</execute_command>`

Write `internal/parsers/cline_test.go`:
```go
package parsers

import (
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestClineParser_Parse(t *testing.T) {
	fixturePath := filepath.Join("testdata", "cline", "tasks", "task-001", "api_conversation_history.json")
	p := &ClineParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "cline"}
	reqs, newState, err := p.Parse(DataSource{Path: fixturePath, Type: "json"}, state)
	if err != nil {
		t.Fatalf("Parse failed: %v", err)
	}

	if len(reqs) == 0 {
		t.Fatalf("expected parsed requests, got 0")
	}

	req := reqs[0]
	if req.Tool != "cline" {
		t.Errorf("expected tool cline, got %s", req.Tool)
	}
	if req.SessionID != "task-001" {
		t.Errorf("expected session task-001, got %s", req.SessionID)
	}
	if req.CacheReadTokens != 2500 {
		t.Errorf("expected 2500 cache read tokens, got %d", req.CacheReadTokens)
	}
	if req.ToolCallCount == 0 {
		t.Errorf("expected tool calls detected, got %d", req.ToolCallCount)
	}

	// Test idempotency
	reqs2, _, err := p.Parse(DataSource{Path: fixturePath, Type: "json"}, newState)
	if err != nil {
		t.Fatalf("Second parse failed: %v", err)
	}
	if len(reqs2) != 0 {
		t.Errorf("expected 0 new requests on second parse, got %d", len(reqs2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers -run TestClineParser`  
Expected: FAIL with `undefined: ClineParser`.

- [ ] **Step 3: Implement `ClineParser` in `internal/parsers/cline.go`**

Implement `ClineParser` extracting turns, tokens, tools (`<execute_command>`, `<write_to_file>`, `<read_file>`), pricing, and registering via `init() { Register(&ClineParser{}) }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers -run TestClineParser`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/cline.go internal/parsers/cline_test.go internal/parsers/testdata/cline/
git commit -m "feat(parsers): implement Cline assistant adapter with prompt caching and tool tracking"
```

---

### Task 5: Roo Code Parser (`roo_code`)

**Files:**
- Create: `internal/parsers/roo_code.go`
- Test: `internal/parsers/roo_code_test.go`
- Testdata: `internal/parsers/testdata/roo_code/tasks/task-roo-01/api_conversation_history.json`

**Interfaces:**
- Implements: `parsers.Parser` with `Name() => "roo_code"`.

- [ ] **Step 1: Write test fixture and failing test**

Write `internal/parsers/roo_code_test.go` asserting that `roo_code` parses independently, records `Tool: "roo_code"`, and registers as a distinct parser from `cline`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers -run TestRooCodeParser`  
Expected: FAIL with `undefined: RooCodeParser`.

- [ ] **Step 3: Implement `RooCodeParser` in `internal/parsers/roo_code.go`**

Implement `RooCodeParser`, registering via `init() { Register(&RooCodeParser{}) }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers -run TestRooCodeParser`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/roo_code.go internal/parsers/roo_code_test.go internal/parsers/testdata/roo_code/
git commit -m "feat(parsers): implement Roo Code assistant adapter"
```

---

### Task 6: Cursor Parser (`cursor`)

**Files:**
- Create: `internal/parsers/cursor.go`
- Test: `internal/parsers/cursor_test.go`
- Testdata: generated via mock `state.vscdb` helper in test.

**Interfaces:**
- Implements: `parsers.Parser` with `Name() => "cursor"`.
- Uses: `internal/parsers/vscdb` and `internal/parsers/tokenestimator`.

- [ ] **Step 1: Write failing test**

Write `internal/parsers/cursor_test.go` setting up a mock `state.vscdb` with `composer.composerData` containing a 2-turn conversation with user prompt, assistant response, and `claude-3-5-sonnet` model. Verify that tokens are estimated and `Status: "estimated"` is set when raw counts are omitted.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers -run TestCursorParser`  
Expected: FAIL with `undefined: CursorParser`.

- [ ] **Step 3: Implement `CursorParser` in `internal/parsers/cursor.go`**

Implement `CursorParser`, registering via `init() { Register(&CursorParser{}) }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers -run TestCursorParser`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/cursor.go internal/parsers/cursor_test.go
git commit -m "feat(parsers): implement Cursor composer parser with hybrid token estimation"
```

---

### Task 7: Windsurf Parser (`windsurf`)

**Files:**
- Create: `internal/parsers/windsurf.go`
- Test: `internal/parsers/windsurf_test.go`
- Testdata: `internal/parsers/testdata/windsurf/cascade/session-001.json`

**Interfaces:**
- Implements: `parsers.Parser` with `Name() => "windsurf"`.

- [ ] **Step 1: Write test fixture and failing test**

Write test fixture and `internal/parsers/windsurf_test.go` asserting Cascade session extraction, step latency, tool executions, and turn costs.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers -run TestWindsurfParser`  
Expected: FAIL with `undefined: WindsurfParser`.

- [ ] **Step 3: Implement `WindsurfParser` in `internal/parsers/windsurf.go`**

Implement `WindsurfParser`, registering via `init() { Register(&WindsurfParser{}) }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers -run TestWindsurfParser`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/windsurf.go internal/parsers/windsurf_test.go internal/parsers/testdata/windsurf/
git commit -m "feat(parsers): implement Windsurf Cascade assistant adapter"
```

---

### Task 8: GitHub Copilot CLI Parser (`copilot_cli`)

**Files:**
- Create: `internal/parsers/copilot_cli.go`
- Test: `internal/parsers/copilot_cli_test.go`
- Testdata: `internal/parsers/testdata/copilot_cli/history.jsonl`

**Interfaces:**
- Implements: `parsers.Parser` with `Name() => "copilot_cli"`.

- [ ] **Step 1: Write test fixture and failing test**

Write test fixture with CLI explain/suggest entries and failing test `internal/parsers/copilot_cli_test.go`.

- [ ] **Step 2: Run test to verify it fails**

Run: `go test -v ./internal/parsers -run TestCopilotCLIParser`  
Expected: FAIL with `undefined: CopilotCLIParser`.

- [ ] **Step 3: Implement `CopilotCLIParser` in `internal/parsers/copilot_cli.go`**

Implement `CopilotCLIParser`, registering via `init() { Register(&CopilotCLIParser{}) }`.

- [ ] **Step 4: Run test to verify it passes**

Run: `go test -v ./internal/parsers -run TestCopilotCLIParser`  
Expected: PASS.

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/copilot_cli.go internal/parsers/copilot_cli_test.go internal/parsers/testdata/copilot_cli/
git commit -m "feat(parsers): implement GitHub Copilot CLI assistant adapter"
```

---

### Task 9: Registry Integration & Full Test Suite Verification

**Files:**
- Modify: `internal/parsers/registry_test.go`
- Modify: `README.md:27-36`

**Interfaces:**
- `parsers.All()` returns 10 registered parsers (`claude_code`, `agy`, `codex`, `aider`, `opencode`, `cline`, `roo_code`, `cursor`, `windsurf`, `copilot_cli`).

- [ ] **Step 1: Update registry test to assert all 10 adapters**

Update `internal/parsers/registry_test.go` to assert that all 10 tools are registered and retrievable via `parsers.Get(name)`.

- [ ] **Step 2: Run test suite with `-race`**

Run: `go test -v -race ./...`  
Expected: PASS across all packages.

- [ ] **Step 3: Update README.md supported CLI assistants table**

Update `README.md` to list the newly supported assistants in the features and supported tools section.

- [ ] **Step 4: Commit**

```bash
git add internal/parsers/registry_test.go README.md
git commit -m "docs: update README and verify all 10 assistant adapters pass registry and race tests"
```
