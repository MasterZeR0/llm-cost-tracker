# Design Specification: Phase 1 IDE & Extension Assistant Adapters

**Date**: 2026-10-18  
**Status**: Approved  
**Topic**: Extension of `llm-cost-tracker` (`llm-track`) with dedicated adapters for Cline, Roo Code, Cursor, Windsurf, and GitHub Copilot CLI.

---

## 1. Overview & Objectives

`llm-cost-tracker` currently tracks costs, tokens, latencies, and cache efficiency for Claude Code, AGY / Gemini CLI, Codex CLI, Aider, and OpenCode.

This design specifies Phase 1 of the assistant adapter expansion, delivering non-invasive, zero-configuration local ingestion for:
1. **Cline** (`saoudrizwan.claude-dev`)
2. **Roo Code** (`rooveterinaryinc.roo-cline`)
3. **Cursor** (`state.vscdb` / Composer)
4. **Windsurf** (`state.vscdb` / Cascade)
5. **GitHub Copilot CLI** (`~/.copilot-cli` / `~/.config/gh/copilot`)

*(Phase 2 for local runtime observability covering Ollama and vLLM will follow in a subsequent milestone).*

---

## 2. Shared Utilities & Architecture

### 2.1 OS-Aware Directory Discovery (`internal/config/paths.go`)
Storage locations for VS Code extensions and forks differ across platforms. A new path resolution utility resolves tool search paths:
- **macOS**: `~/Library/Application Support/Code/User/globalStorage`, `~/Library/Application Support/Cursor/User/workspaceStorage`, `~/Library/Application Support/Windsurf/User/...`
- **Linux**: `~/.config/Code/User/globalStorage`, `~/.config/Cursor/User/...`, `~/.config/Windsurf/User/...`, honoring `$XDG_CONFIG_HOME`.
- **Windows**: `%APPDATA%\Code\User\globalStorage`, `%APPDATA%\Cursor\User\...`, honoring `%APPDATA%`.

If custom paths are specified in `config.yaml`, they override automatic OS path probing.

### 2.2 Shared `vscdb` Helper (`internal/parsers/vscdb/vscdb.go`)
Reads SQLite state databases (`state.vscdb`) used by Cursor, Windsurf, and VS Code extensions.
- **Connection**: Opened with URI parameters `?mode=ro&immutable=1` to guarantee read-only access and prevent locking the IDE.
- **API**:
  ```go
  type Reader struct { db *sql.DB }
  func Open(path string) (*Reader, error)
  func (r *Reader) GetKey(key string) ([]byte, error)
  func (r *Reader) GetKeysLike(prefix string) (map[string][]byte, error)
  func (r *Reader) Close() error
  ```

### 2.3 Shared Token Estimator (`internal/parsers/tokenestimator/estimator.go`)
Closed-source proxy assistants (such as Cursor or Copilot CLI) frequently omit exact API token usage from local chat logs.
- When native token metrics exist in the parsed payload, they are ingested verbatim and marked with `Status: "success"`.
- When token metrics are missing or 0:
  - Input tokens are estimated from user prompt text at ~4.0 characters per token.
  - Output tokens are estimated from assistant response text at ~3.8 characters per token.
  - The record is marked with `Status: "estimated"`.
  - Cost is calculated against the detected model using `internal/pricing`.

---

## 3. Dedicated Parser Implementations

### 3.1 Cline Parser (`internal/parsers/cline.go`)
- **Name**: `cline`
- **Data Source**: Scans for `.../saoudrizwan.claude-dev/tasks/<task-id>/api_conversation_history.json`.
- **Turn Parsing**:
  - Each message turn yields a `store.Request` with `SessionID: <task-id>`, `StepIndex`, and timestamp.
  - Extracts exact `input_tokens`, `output_tokens`, `cache_creation_input_tokens` (cache write), and `cache_read_input_tokens` (cache read).
  - Evaluates native `cost` if present; otherwise calculates cost via `pricing.CalculateCost`.
- **Tool Observability**:
  - Detects tool call XML blocks (`<execute_command>`, `<read_file>`, `<write_to_file>`, `<replace_in_file>`, `<browser_action>`, `<use_mcp_tool>`).
  - Detects tool error status from subsequent tool response content.
  - Increments `ToolCallCount` and `ToolErrorCount`.

### 3.2 Roo Code Parser (`internal/parsers/roo_code.go`)
- **Name**: `roo_code`
- **Data Source**: Scans for `.../rooveterinaryinc.roo-cline/tasks/<task-id>/api_conversation_history.json`.
- **Turn Parsing**:
  - Completely independent parser module from Cline, tagged as `tool: "roo_code"`.
  - Ingests Roo Code custom modes, tools, and token metrics.
  - Supports prompt caching analytics for Anthropic / OpenRouter models.

### 3.3 Cursor Parser (`internal/parsers/cursor.go`)
- **Name**: `cursor`
- **Data Source**: Scans `workspaceStorage/*/state.vscdb` and `globalStorage/state.vscdb`.
- **Turn Parsing**:
  - Uses `vscdb` reader to query `composer.composerData` and `workbench.panel.aichat.chatdata`.
  - Parses composer session trees: `composerId`, conversation bubbles, message chronology.
  - Identifies model strings (e.g., `claude-3-5-sonnet`, `claude-3-7-sonnet`, `gpt-4o`, `cursor-small`).
  - Extracts tool calls (file edits, terminal executions) from assistant bubble actions.
  - Applies `tokenestimator` if raw token counts are omitted.

### 3.4 Windsurf Parser (`internal/parsers/windsurf.go`)
- **Name**: `windsurf`
- **Data Source**: Scans `Windsurf/User/workspaceStorage/*/state.vscdb` and Cascade session logs in `~/.codeium/windsurf/cascade/`.
- **Turn Parsing**:
  - Ingests Cascade multi-step sessions and turns.
  - Maps tool executions (`run_command`, `edit_file`, `view_code`, `search_codebase`) and error results.
  - Ingests latency and timestamp chronology per step.

### 3.5 GitHub Copilot CLI Parser (`internal/parsers/copilot_cli.go`)
- **Name**: `copilot_cli`
- **Data Source**: Scans `~/.copilot-cli/` and `~/.config/gh/copilot/` session logs.
- **Turn Parsing**:
  - Ingests CLI queries, explanations, and suggested terminal commands.
  - Resolves model to `gpt-4o` family.
  - Calculates tokens and costs via `tokenestimator` and pricing engine.

---

## 4. Incremental Sync & Deduplication

### 4.1 Incremental State Tracking
Each parser tracks its progress using `store.SyncState`:
- **JSON Files (Cline, Roo Code, Copilot CLI)**:
  - Tracks `LastOffset` and `LastModified`.
  - If a file is rotated or truncated (`file.Size < state.LastOffset`), `LastOffset` safely resets to 0.
- **SQLite Databases (Cursor, Windsurf)**:
  - Tracks file `LastModified` and last processed bubble/row identifier in `LastRowID`.

### 4.2 Idempotency
- Unique turn ID format: `fmt.Sprintf("%s:%s:%d", toolName, sessionID, stepIndex)`.
- Store operations use `INSERT OR REPLACE` to safely update in-flight turns as streaming tokens finish without duplicating requests.

---

## 5. Testing & Verification

1. **Unit Tests with Fixtures**:
   - `internal/parsers/cline_test.go`: Ingests sample Cline task folder with prompt caching and tool calls.
   - `internal/parsers/roo_code_test.go`: Validates Roo Code independent registration and metrics.
   - `internal/parsers/cursor_test.go`: Ingests mock `state.vscdb` with composer session bubbles.
   - `internal/parsers/windsurf_test.go`: Ingests sample Cascade session data.
   - `internal/parsers/copilot_cli_test.go`: Ingests Copilot CLI session history.
   - `internal/parsers/vscdb_test.go`: Validates read-only access and key extraction.
   - `internal/parsers/estimator_test.go`: Validates token estimation heuristics.
2. **Idempotency Verification**: Running `Parse()` twice on unchanged data produces 0 new records.
3. **Integration Verification**:
   - `parsers.All()` contains all 10 registered adapters.
   - `go test -v -race ./...` runs cleanly with zero failures.
