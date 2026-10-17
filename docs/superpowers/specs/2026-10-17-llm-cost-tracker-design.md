# LLM Cost Tracker — Design Specification

## Overview

A Go-based CLI tool and daemon that tracks cost, tokens, and latency across CLI-based LLM coding assistants (Claude Code, Aider, Codex CLI, AGY, OpenCode, and more). It reads usage data that these tools already write to the local filesystem, unifies it into a single SQLite database, and presents it through a terminal summary and web dashboard.

### Goals

- **Zero-friction tracking**: Reads existing log files — no workflow changes, no wrappers, no proxies
- **Unified cross-tool view**: One database, one dashboard for all your LLM CLI tools
- **Always-on**: Background daemon watches for new data in real-time via filesystem events
- **Extensible**: Adding a new tool means writing one parser module (~100-200 lines of Go)
- **Privacy-conscious**: Prompt/response text storage is opt-in and off by default

### Non-Goals

- Multi-user / authentication (this is a personal/team tool)
- Cloud storage or remote sync (local SQLite only)
- Real-time WebSocket push to dashboard (poll/refresh is sufficient for v1)
- Wrapping or proxying LLM API calls (we read what the tools already write)
- Tracking Python SDK / programmatic API calls (CLI tools only for v1)

---

## Architecture

```
┌──────────────────────────────────────────────┐
│              CLI: llm-track                   │
│  llm-track daemon start|stop|status           │
│  llm-track dashboard                          │
│  llm-track sync                               │
│  llm-track summary [--period=today|week|month] │
│  llm-track tools                              │
│  llm-track config [init]                      │
└──────────┬───────────────────────┬────────────┘
           │                       │
┌──────────▼──────────┐  ┌────────▼─────────────┐
│   Watcher Daemon    │  │   Dashboard Server    │
│  (background proc)  │  │  (FastAPI-style Go    │
│                     │  │   HTTP server)        │
│ • fsnotify watchers │  │                       │
│   on tool data dirs │  │ • REST API for data   │
│ • Periodic polling  │  │ • Serves HTML/JS UI   │
│   as fallback (30s) │  │ • Overview cards      │
│ • Routes file       │  │ • Analysis charts     │
│   changes to        │  │                       │
│   parsers           │  │                       │
└──────────┬──────────┘  └────────┬─────────────┘
           │                       │
┌──────────▼───────────────────────▼────────────┐
│                   Core Engine                  │
│  ┌─────────────┐ ┌────────────┐ ┌───────────┐ │
│  │   Parsers   │ │  Pricing   │ │   Store   │ │
│  │  (per-tool  │ │  Engine    │ │  (SQLite) │ │
│  │   plugins)  │ │            │ │           │ │
│  └─────────────┘ └────────────┘ └───────────┘ │
└────────────────────────────────────────────────┘
```

### Technology Choices

| Component | Choice | Rationale |
|-----------|--------|-----------|
| Language | Go | Single binary distribution, natural daemon support, low memory footprint, fast builds |
| Storage | SQLite | Zero-config, local, fast for this scale, `go-sqlite3` via CGo |
| File watching | `fsnotify` | Mature, cross-platform, idiomatic Go |
| Web server | Go `net/http` + `html/template` | No external framework needed for this scale |
| Charts | Chart.js (CDN) | No build step, good defaults, widely used |
| Frontend | Vanilla JS + minimal CSS | No npm, no build tools, ships embedded in the binary |
| Logging | `slog` (Go stdlib) | Structured logging, zero dependencies |
| Log rotation | `lumberjack` | Proven Go log rotation library |
| CLI framework | `cobra` | Standard Go CLI framework |

---

## Data Model

### SQLite Schema

```sql
-- Each LLM interaction/turn gets one row
CREATE TABLE requests (
    id                 TEXT PRIMARY KEY,        -- UUID
    tool               TEXT NOT NULL,           -- 'claude_code', 'aider', 'codex', 'agy', 'opencode'
    session_id         TEXT,                    -- tool-specific session identifier
    timestamp          DATETIME NOT NULL,       -- when the request occurred
    model              TEXT,                    -- 'claude-opus-5', 'gpt-4o', etc.

    -- Token counts
    input_tokens       INTEGER DEFAULT 0,
    output_tokens      INTEGER DEFAULT 0,
    cache_read_tokens  INTEGER DEFAULT 0,
    cache_write_tokens INTEGER DEFAULT 0,
    total_tokens       INTEGER DEFAULT 0,      -- computed: input + output

    -- Cost & performance
    cost_usd           REAL,                   -- dollar cost (from tool or computed via pricing table)
    latency_ms         INTEGER,                -- duration in milliseconds
    status             TEXT DEFAULT 'success', -- 'success', 'error'

    -- Optional (opt-in via config)
    prompt_text        TEXT,                   -- user prompt (stored only if store_content=true)
    response_text      TEXT,                   -- LLM response (stored only if store_content=true)

    -- Metadata
    source_file        TEXT,                   -- which log file this came from
    raw_data           TEXT,                   -- original JSON for debugging
    imported_at        DATETIME DEFAULT CURRENT_TIMESTAMP,

    -- Deduplication
    UNIQUE(tool, session_id, timestamp, model)
);

-- Pricing table for tools that don't report cost natively
CREATE TABLE pricing (
    model_pattern       TEXT PRIMARY KEY,      -- glob pattern: 'claude-opus-*', 'gpt-4o'
    input_per_mtok      REAL NOT NULL,         -- $ per million input tokens
    output_per_mtok     REAL NOT NULL,         -- $ per million output tokens
    cache_read_per_mtok REAL DEFAULT 0,
    cache_write_per_mtok REAL DEFAULT 0,
    effective_date      DATE NOT NULL,
    source              TEXT                   -- 'manual', 'bundled'
);

-- Track what we've already imported (incremental sync)
CREATE TABLE sync_state (
    source_path    TEXT PRIMARY KEY,           -- file path being watched
    tool           TEXT NOT NULL,
    last_offset    INTEGER DEFAULT 0,          -- byte offset for JSONL files
    last_modified  DATETIME,                   -- file mtime
    last_row_id    TEXT,                       -- last imported row ID for SQLite sources
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

-- Indexes for dashboard queries
CREATE INDEX idx_requests_timestamp ON requests(timestamp);
CREATE INDEX idx_requests_tool ON requests(tool);
CREATE INDEX idx_requests_model ON requests(model);
CREATE INDEX idx_requests_tool_timestamp ON requests(tool, timestamp);
```

### Design Decisions

- **Dedup via UNIQUE constraint**: The combination of (tool, session_id, timestamp, model) uniquely identifies a turn. Re-importing the same data is idempotent via `INSERT OR IGNORE`.
- **sync_state table**: Tracks byte offsets for JSONL files (Claude Code, Aider, AGY) so only new lines are parsed. For SQLite sources (Codex, OpenCode), tracks the last imported row. This makes re-scanning fast.
- **Pricing table**: Ships with bundled pricing for major models. Used to compute cost for tools that don't report it natively (AGY, Codex). Users can update pricing via config or CLI.
- **Cache token tracking**: Claude Code reports cache_read and cache_write tokens separately, with different pricing. These are first-class fields.
- **Prompt/response storage**: Off by default for privacy. Enabled via `store_content: true` in config.

---

## Parser Plugin System

### Interface

```go
// Parser is the interface every tool parser implements.
type Parser interface {
    // Name returns the tool identifier (e.g., "claude_code", "aider").
    Name() string

    // Discover finds all data sources for this tool on the local machine.
    // Returns paths to session files/databases it found.
    Discover(config ToolConfig) ([]DataSource, error)

    // Parse reads new data from a source, starting from the given sync state.
    // Returns parsed requests and updated sync state.
    Parse(source DataSource, state SyncState) ([]Request, SyncState, error)
}

// DataSource represents a file or database that contains tool usage data.
type DataSource struct {
    Path string    // file path
    Type string    // "jsonl" or "sqlite"
}

// ToolConfig holds user-configurable settings for a tool.
type ToolConfig struct {
    Enabled bool
    Paths   []string          // override default discovery paths
    Extra   map[string]string // tool-specific settings
}
```

### Per-Tool Implementation

| Tool | Discovery Path | Format | Parsing Strategy | Has Native Cost? |
|------|---------------|--------|------------------|-----------------|
| **Claude Code** | `~/.claude/projects/**/*.jsonl` | JSONL | Filter `type=assistant` events. Extract `costUSD`, `message.usage.{input_tokens, output_tokens, cache_read_input_tokens, cache_creation_input_tokens}`, `message.model`, `durationMs` | Yes (`costUSD`) |
| **Aider** | Configurable analytics log path, or `<project>/.aider.chat.history.md` | JSONL (preferred) or Markdown | JSONL: extract `prompt_tokens`, `completion_tokens`, `cost`, `main_model`, `time`. Markdown fallback: parse `Tokens: X sent, Y received. Cost: $Z` lines | Yes (`cost`) |
| **Codex CLI** | `~/.codex/state_5.sqlite` | SQLite | Query `threads` table for `tokens_used`, `model_provider`, `created_at`, `updated_at`. Cost computed via pricing table. | No |
| **OpenCode** | `~/.local/share/opencode/opencode.db` | SQLite | Query sessions/messages for token counts, model info. Cost computed via pricing table. | No |
| **AGY** | `~/.gemini/antigravity-cli/brain/*/...transcript.jsonl` | JSONL | Filter `PLANNER_RESPONSE` events. Extract `input_tokens`, `output_tokens`, `cache_read_tokens`. Model from conversation metadata. Cost computed via pricing table. | No |

### Adding a New Tool

1. Create `internal/parsers/newtool.go`
2. Implement the 3-method `Parser` interface
3. Register in `internal/parsers/registry.go`: `Register(&NewToolParser{})`
4. Add default paths to bundled config

No changes needed to daemon, store, dashboard, or any other component.

---

## Daemon Architecture

### Lifecycle

```
llm-track daemon start
  → Fork background process
  → Write PID to ~/.local/share/llm-cost-tracker/daemon.pid
  → Start fsnotify watchers on all discovered tool data directories
  → Start periodic poll ticker (30s interval)
  → Log to ~/.local/share/llm-cost-tracker/daemon.log

llm-track daemon stop
  → Read PID from file
  → Send SIGTERM
  → Daemon: flush pending writes, clean up PID file, exit

llm-track daemon status
  → Check PID file + process liveness
  → Show: running/stopped, uptime, last sync time per tool, watched paths
```

### Event Processing Flow

```
FS Event or Poll Tick
  → Identify which tool the changed file belongs to
  → Load sync_state for that file
  → Call parser.Parse(source, state)
  → For each returned Request:
      → If tool doesn't provide cost: look up pricing, compute cost
      → INSERT OR IGNORE into requests table
  → Update sync_state with new offset/row ID
```

### Hybrid Watch Strategy

- **Primary**: `fsnotify` watchers on tool data directories for instant detection
- **Fallback**: 30-second periodic poll that rescans all sources (catches any events fsnotify missed, handles new session files created in unwatched subdirectories)
- Both mechanisms are always active

---

## CLI Commands

```
llm-track                          # alias for 'summary' — show today's stats
llm-track daemon start             # start background watcher daemon
llm-track daemon stop              # stop daemon
llm-track daemon status            # show daemon status + last sync times
llm-track sync                     # one-shot: scan all sources, import new data, exit
llm-track dashboard                # start web dashboard server (foreground, localhost:8384)
llm-track summary                  # today's cost/token summary in terminal
llm-track summary --period=week    # last 7 days
llm-track summary --period=month   # last 30 days
llm-track tools                    # list detected tools and their data paths
llm-track config                   # show current configuration
llm-track config init              # create default config file with auto-discovered tools
```

### Terminal Summary Output

```
╭─────────────────────────────────────────────╮
│           LLM Cost Tracker — Today          │
├─────────────────────────────────────────────┤
│  Total Requests: 47                         │
│  Total Tokens:   1,234,567                  │
│  Total Cost:     $12.34                     │
│  Avg Latency:    2.3s                       │
├─────────────────────────────────────────────┤
│  By Tool:                                   │
│    Claude Code   32 reqs   $8.21   890K tok │
│    AGY            9 reqs   $2.45   230K tok │
│    Aider          6 reqs   $1.68   114K tok │
├─────────────────────────────────────────────┤
│  By Model:                                  │
│    claude-opus-5    28 reqs  $9.50          │
│    claude-sonnet-5  14 reqs  $2.10          │
│    gpt-4o            5 reqs  $0.74          │
╰─────────────────────────────────────────────╯
```

---

## Dashboard

### Technology

- Go `net/http` serves the dashboard on `localhost:8384`
- Single HTML page rendered via `html/template`, embedded in the binary
- Chart.js loaded from CDN for charts
- Vanilla JS for API calls and interactivity
- Minimal CSS (Pico CSS classless baseline or custom)
- Auto-refresh: polls API every 60 seconds

### Layout

**Header**: "LLM Cost Tracker" + time period selector (Today / 7 Days / 30 Days / All Time)

**Overview Cards** (top row, 4 cards):

| Total Requests | Total Tokens | Total Cost | Avg Latency |
|:-:|:-:|:-:|:-:|
| 247 | 5.2M | $47.83 | 2.1s |

**Analysis Charts** (2×2 grid):

| Position | Chart | Type |
|----------|-------|------|
| Top-left | Cost Over Time | Line chart, stacked by tool, daily granularity |
| Top-right | Cost by Model | Horizontal bar chart |
| Bottom-left | Token Usage by Model | Bar chart showing input vs output tokens per model |
| Bottom-right | Tool Usage | Donut chart showing request count distribution |

**Request Log** (bottom): Sortable, filterable table of recent requests. Columns: Time, Tool, Model, Input Tokens, Output Tokens, Cost, Latency.

### REST API

```
GET /api/overview?period=today|week|month|all
  → { total_requests, total_tokens, total_cost, avg_latency_ms }

GET /api/cost-over-time?period=week&group_by=tool
  → [{ date, tool, cost }]

GET /api/cost-by-model?period=week
  → [{ model, cost, request_count }]

GET /api/tokens-by-model?period=week
  → [{ model, input_tokens, output_tokens }]

GET /api/tool-usage?period=week
  → [{ tool, request_count, total_cost }]

GET /api/requests?period=week&tool=&model=&page=1&per_page=50
  → { requests: [...], total, page, per_page }
```

---

## Configuration

Location: `~/.config/llm-cost-tracker/config.yaml`

```yaml
# Data directory for the unified database
data_dir: ~/.local/share/llm-cost-tracker

# Store prompt/response text (default: false for privacy)
store_content: false

# Log level: debug, info, warn, error
log_level: info

# Tools to track (auto-discovered if paths exist on first run)
tools:
  claude_code:
    enabled: true
    paths:
      - ~/.claude/projects/
  aider:
    enabled: true
    analytics_log: ""  # set to a path to use --analytics-log JSONL; empty = parse chat history
  codex:
    enabled: true
    paths:
      - ~/.codex/state_5.sqlite
  agy:
    enabled: true
    paths:
      - ~/.gemini/antigravity-cli/brain/
  opencode:
    enabled: true
    paths:
      - ~/.local/share/opencode/opencode.db

# Daemon settings
daemon:
  poll_interval: 30s
  watch_fs_events: true

# Dashboard settings
dashboard:
  port: 8384
  host: 127.0.0.1
```

`llm-track config init` auto-discovers which tools have data on disk and generates this file with only the detected tools enabled.

---

## Error Handling

| Scenario | Behavior |
|----------|----------|
| Tool data directory doesn't exist | Skip silently, log at debug level. `llm-track tools` shows "not found" |
| JSONL parse error on a line | Skip the line, log warning with file + line number, continue parsing |
| SQLite database locked | Retry with backoff (3 attempts: 1s, 2s, 4s). Log warning if all fail, continue with other tools |
| Unknown model in pricing lookup | Log warning, store request with `cost_usd = NULL`. Dashboard shows "—" for cost |
| Duplicate data on re-import | `INSERT OR IGNORE` via UNIQUE constraint — silently skip |
| Daemon already running | Detect existing PID, print message, exit with error code |
| Permission denied on data files | Log error with clear message, skip that source |
| Disk full / DB write failure | Log error, daemon stays alive but pauses writes until space available |
| File format change (tool update) | Parser validates expected fields. Missing fields → warning + partial import. Unrecognized format → skip file, log error suggesting parser update needed |

---

## Testing Strategy

### Unit Tests

- **Parsers**: Test fixtures with anonymized sample data in each tool's format. Verify correct extraction of tokens, cost, model, timestamps. Verify incremental parsing (offset tracking) works.
- **Pricing engine**: Test cost computation for various models including cache token pricing. Test unknown model fallback.
- **Store**: Test insert, dedup, query functions for each API endpoint.
- **Dashboard API**: Test each endpoint with known data, verify aggregation and period filtering.

### Integration Tests

- End-to-end: populate temp directory with fixture files → run sync → verify database → query API → verify response
- Daemon start/stop lifecycle test

### Test Fixtures

- Anonymized sample data for each tool format stored in `testdata/` per parser
- Real JSONL/SQLite structure with fake content

---

## Project Structure

```
llm-cost-tracker/
├── cmd/
│   └── llm-track/
│       └── main.go              # CLI entrypoint
├── internal/
│   ├── cli/                     # Cobra command definitions
│   │   ├── root.go
│   │   ├── daemon.go
│   │   ├── sync.go
│   │   ├── summary.go
│   │   ├── dashboard.go
│   │   ├── tools.go
│   │   └── config.go
│   ├── config/
│   │   └── config.go            # Config loading and defaults
│   ├── daemon/
│   │   └── daemon.go            # Watcher daemon logic
│   ├── parsers/
│   │   ├── parser.go            # Parser interface and types
│   │   ├── registry.go          # Parser registration
│   │   ├── claude_code.go
│   │   ├── aider.go
│   │   ├── codex.go
│   │   ├── agy.go
│   │   ├── opencode.go
│   │   └── testdata/            # Test fixtures per parser
│   ├── pricing/
│   │   ├── pricing.go           # Pricing engine
│   │   └── defaults.go          # Bundled model pricing
│   ├── store/
│   │   ├── store.go             # SQLite operations
│   │   ├── schema.go            # Schema creation/migration
│   │   └── queries.go           # Query functions for API
│   └── dashboard/
│       ├── server.go            # HTTP server and API handlers
│       ├── handlers.go          # API endpoint handlers
│       └── static/              # Embedded static files
│           ├── index.html
│           ├── app.js
│           └── style.css
├── go.mod
├── go.sum
├── README.md
└── docs/
    └── superpowers/
        └── specs/
            └── 2026-10-17-llm-cost-tracker-design.md  # this file
```

---

## Key Dependencies

| Package | Purpose |
|---------|---------|
| `github.com/spf13/cobra` | CLI framework |
| `github.com/fsnotify/fsnotify` | File system event watching |
| `github.com/mattn/go-sqlite3` | SQLite driver (CGo) |
| `gopkg.in/yaml.v3` | YAML config parsing |
| `github.com/google/uuid` | UUID generation for request IDs |
| `gopkg.in/natefinsh/lumberjack.v2` | Log rotation |
