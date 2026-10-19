# GEMINI.md - Agent Guidelines for `llm-cost-tracker`

## Project Overview

`llm-cost-tracker` (`llm-track`) is a lightweight, non-invasive Go CLI and background daemon that tracks LLM usage costs, tokens, latencies, cache efficiency, and agentic workflows across AI coding assistants and IDEs (Claude Code, AGY / Gemini CLI, Codex, Aider, OpenCode, Cline, Roo Code, Cursor, Windsurf, GitHub Copilot CLI).

### Core Stack
- **Language**: Go 1.27+
- **CLI Framework**: `github.com/spf13/cobra`
- **Embedded Database**: SQLite via `github.com/mattn/go-sqlite3` (CGO required)
- **Filesystem Monitoring**: `github.com/fsnotify/fsnotify` with offset tracking and fallback polling
- **Embedded Web UI**: Go standard library `net/http` serving Chart.js dashboard
- **Configuration**: YAML via `gopkg.in/yaml.v3` (`~/.llm-track/config.yaml`)

---

## Directory Architecture

```
llm-cost-tracker/
├── cmd/
│   └── llm-track/            # Main entrypoint (main.go)
├── internal/
│   ├── cli/                  # Cobra commands (root, sync, summary, sessions, tools, daemon, dashboard)
│   ├── config/               # Config file loading, defaults, path expansion (~/.llm-track/)
│   ├── daemon/               # Background fsnotify watcher, offset tracking, polling fallback
│   ├── dashboard/            # Embedded HTTP server, API endpoints, static UI assets
│   ├── parsers/              # Tool-specific log & database parsers
│   │   ├── tokenestimator/   # Heuristic BPE/character token estimation for unmetered tools
│   │   ├── vscdb/            # SQLite parser helpers for VS Code state.vscdb databases
│   │   ├── testdata/         # Sample fixture logs for parser unit tests
│   │   └── registry.go       # Parser registration & discovery interface
│   ├── pricing/              # Model pricing engine, catalog, cache discount computations
│   └── store/                # SQLite storage layer, migrations, analytical queries
├── test/                     # End-to-end integration and resilience test suites
└── bin/                      # Compiled binary outputs (gitignored)
```

---

## Essential Commands

### Build & Run
```bash
# Compile binary to bin/
go build -o bin/llm-track ./cmd/llm-track

# Install globally
go install ./cmd/llm-track

# Run CLI commands directly
./bin/llm-track sync
./bin/llm-track summary --period=today
./bin/llm-track sessions
./bin/llm-track tools
./bin/llm-track dashboard --port=8080
./bin/llm-track daemon --foreground
```

### Testing & Verification
```bash
# Run all unit tests
go test ./...

# Run all tests with race detector enabled
go test -race ./...

# Run parser-specific tests
go test -v ./internal/parsers/...

# Run end-to-end and resilience tests
go test -v ./test/...
```

### Code Hygiene
```bash
# Format all code
go fmt ./...

# Static analysis
go vet ./...

# Clean dependencies
go mod tidy
```

---

## Code Exploration Protocol (Graph-RAG)

When asked to implement features, fix bugs, or understand unfamiliar modules:

1. **Do NOT use recursive grep or find broad patterns across the whole repo.**
2. **Phase 1: Semantic Discovery**
   - Run vector retrieval first:
     `code-search query "<concept or logic description>"`
   - Identify the primary entrypoint symbol(s) or file paths.

3. **Phase 2: Structural Expansion (Graphify)**
   - Query the Graphify relationship graph for the identified symbol:
     `node scripts/query-graph.mjs "<symbol_name>"`
   - Review incoming callers (to check blast radius) and outgoing dependencies.

4. **Phase 3: Targeted Reading**
   - Only view or edit the files returned by Phase 1 and Phase 2.
   - Do not read whole directories when single-symbol contexts are sufficient.

---

## Key Conventions & Engineering Guidelines

### 1. Non-Invasive Data Ingestion
- **Strictly Read-Only**: Parsers must never modify, write to, lock, or corrupt log files or databases belonging to external IDEs/assistants (`~/.claude`, `~/.gemini`, `~/.codex`, Cursor/Windsurf `state.vscdb`, etc.).
- **Resilient Parsing**: Files may be concurrently written by active assistant sessions. Always handle partial lines, trailing incomplete JSON, or temporary file locks gracefully without crashing.

### 2. Idempotency & Deduplication
- Repeated `llm-track sync` runs or ongoing daemon syncs must not produce duplicate records in SQLite.
- Use deterministic uniqueness keys (session ID, message turn ID, timestamp, and hash fingerprints) when inserting events, turns, and tool calls.

### 3. Database & Concurrency
- `mattn/go-sqlite3` uses CGO. Keep transactions concise.
- Ensure foreign keys are enabled (`PRAGMA foreign_keys = ON;`) and WAL mode is set (`PRAGMA journal_mode = WAL;`) for concurrent read/write operations between the CLI, daemon, and dashboard server.
- Close database rows and prepared statements properly to avoid connection leakage.

### 4. Pricing & Model Fallbacks
- Pricing catalog matches models using normalized prefix matching (e.g., `claude-3-5-sonnet`, `gemini-2.0-flash`, `gpt-4o`).
- If an unknown or custom model identifier is encountered, fall back to safe zero-cost metrics with clear logging rather than failing the import.

### 5. Adding New Assistant Parsers
- Implement the `Parser` interface defined in `internal/parsers/parser.go`.
- Register the parser in `internal/parsers/registry.go` inside `init()`.
- Provide fixture files under `internal/parsers/testdata/` and add corresponding unit test coverage in `internal/parsers/<tool>_test.go`.
