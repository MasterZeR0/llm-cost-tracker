# LLM Cost Tracker Implementation Plan

> **For agentic workers:** REQUIRED SUB-SKILL: Use superpowers:subagent-driven-development (recommended) or superpowers:executing-plans to implement this plan task-by-task. Steps use checkbox (`- [ ]`) syntax for tracking.

**Goal:** Build a Go CLI tool and background daemon (`llm-track`) that continuously monitors and ingests token usage, latency, and costs from CLI coding assistants (Claude Code, AGY, Codex, Aider, OpenCode) into a local SQLite database, providing both terminal summaries and a browser dashboard.

**Architecture:** A modular Go application with pluggable parsers for each CLI tool's local log/sqlite format, a pricing engine for computing costs from tokens, an SQLite storage engine with deduplication and incremental offset tracking, an fsnotify + polling watcher daemon, a Cobra-based CLI, and a built-in `net/http` static & REST API dashboard.

**Tech Stack:** Go (1.24+), SQLite (`github.com/mattn/go-sqlite3`), Cobra (`github.com/spf13/cobra`), fsnotify (`github.com/fsnotify/fsnotify`), YAML (`gopkg.in/yaml.v3`), Google UUID (`github.com/google/uuid`), lumberjack (`gopkg.in/natefinsh/lumberjack.v2`), Chart.js (CDN).

**Spec:** [docs/superpowers/specs/2026-10-17-llm-cost-tracker-design.md](file:///Users/nehaverma/orca/projects/llm-cost-tracker/docs/superpowers/specs/2026-10-17-llm-cost-tracker-design.md)

## Global Constraints

- Storage: SQLite local database at `~/.local/share/llm-cost-tracker/tracker.db` by default.
- Zero-config / non-invasive: Read-only access to source logs; never modify tool state or wrap execution unless explicitly requested.
- Deduplication: Deduplication enforced at the database level by `UNIQUE(tool, session_id, timestamp, model)`.
- Incremental parsing: Persist byte offset for JSONL and row ID / timestamp for SQLite in `sync_state`.
- Privacy default: `store_content` is `false` by default; prompt/response bodies are not stored unless enabled in config.
- Daemon safety: Graceful shutdown on SIGTERM/SIGINT with PID file cleanup at `~/.local/share/llm-cost-tracker/daemon.pid`.

## Review Focus

1. Corrupted or partially written JSONL line while a tool is actively logging: parser must skip the incomplete line without failing or advancing the offset past unread data.
2. Concurrent database access between the background daemon writer and CLI / web dashboard readers: SQLite connection must enable WAL mode and set busy timeout.
3. Unknown model string without pricing info: store with `cost_usd = NULL` and prevent zero/infinite cost skew in aggregations.
4. Duplicate session log replay or re-scan: `INSERT OR IGNORE` must guarantee zero duplicate records or inflated totals.
5. Large log files (multi-megabyte transcripts): incremental byte offset reading must not load entire files into memory.

---

### Task 1: Project Scaffolding & Configuration Module

**Files:**
- Create: `go.mod`
- Create: `internal/config/config.go`
- Test: `internal/config/config_test.go`

**Interfaces:**
- Produces: `Config`, `ToolConfig`, `DaemonConfig`, `DashboardConfig` structs in `package config`.
- Produces: `func LoadConfig(path string) (*Config, error)`, `func DefaultConfig() *Config`, `func ExpandHome(path string) string`.

- [ ] **Step 1: Initialize go.mod and add core dependencies**

Run:
```bash
go mod init github.com/nehaverma/llm-cost-tracker
go get github.com/spf13/cobra@latest
go get github.com/mattn/go-sqlite3@latest
go get github.com/fsnotify/fsnotify@latest
go get gopkg.in/yaml.v3@latest
go get github.com/google/uuid@latest
go get gopkg.in/natefinsh/lumberjack.v2@latest
```

- [ ] **Step 2: Write failing test for configuration loading and defaults**

Create `internal/config/config_test.go`:
```go
package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.DataDir == "" {
		t.Fatal("expected default DataDir to be non-empty")
	}
	if cfg.StoreContent {
		t.Fatal("expected default StoreContent to be false")
	}
	if !cfg.Tools["claude_code"].Enabled {
		t.Fatal("expected claude_code tool to be enabled by default")
	}
	if cfg.Dashboard.Port != 8384 {
		t.Fatalf("expected dashboard port 8384, got %d", cfg.Dashboard.Port)
	}
}

func TestLoadConfig_CustomFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
data_dir: /custom/data
store_content: true
log_level: debug
dashboard:
  port: 9000
  host: 0.0.0.0
tools:
  claude_code:
    enabled: false
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.DataDir != "/custom/data" {
		t.Fatalf("expected /custom/data, got %s", cfg.DataDir)
	}
	if !cfg.StoreContent {
		t.Fatal("expected StoreContent true")
	}
	if cfg.Dashboard.Port != 9000 {
		t.Fatalf("expected port 9000, got %d", cfg.Dashboard.Port)
	}
	if cfg.Tools["claude_code"].Enabled {
		t.Fatal("expected claude_code disabled")
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/config/... -v`
Expected: FAIL (compilation errors, package not found)

- [ ] **Step 4: Implement minimal configuration code**

Create `internal/config/config.go`:
```go
package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ToolConfig struct {
	Enabled      bool              `yaml:"enabled"`
	Paths        []string          `yaml:"paths"`
	AnalyticsLog string            `yaml:"analytics_log,omitempty"`
	Extra        map[string]string `yaml:"extra,omitempty"`
}

type DaemonConfig struct {
	PollInterval   time.Duration `yaml:"poll_interval"`
	WatchFSEvents  bool          `yaml:"watch_fs_events"`
}

type DashboardConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type Config struct {
	DataDir      string                `yaml:"data_dir"`
	StoreContent bool                  `yaml:"store_content"`
	LogLevel     string                `yaml:"log_level"`
	Tools        map[string]ToolConfig `yaml:"tools"`
	Daemon       DaemonConfig          `yaml:"daemon"`
	Dashboard    DashboardConfig       `yaml:"dashboard"`
}

func ExpandHome(path string) string {
	if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "llm-cost-tracker")

	return &Config{
		DataDir:      dataDir,
		StoreContent: false,
		LogLevel:     "info",
		Tools: map[string]ToolConfig{
			"claude_code": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".claude", "projects")},
			},
			"aider": {
				Enabled:      true,
				AnalyticsLog: "",
			},
			"codex": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".codex", "state_5.sqlite")},
			},
			"agy": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".gemini", "antigravity-cli", "brain")},
			},
			"opencode": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".local", "share", "opencode", "opencode.db")},
			},
		},
		Daemon: DaemonConfig{
			PollInterval:  30 * time.Second,
			WatchFSEvents: true,
		},
		Dashboard: DashboardConfig{
			Port: 8384,
			Host: "127.0.0.1",
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	expandedPath := ExpandHome(path)
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	cfg.DataDir = ExpandHome(cfg.DataDir)
	return cfg, nil
}
```

- [ ] **Step 5: Run tests and verify they pass**

Run: `go test ./internal/config/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add go.mod go.sum internal/config/
git commit -m "feat(config): add default configuration loader and path expansion"
```

---

### Task 2: Storage Layer & Schema Migrations

**Files:**
- Create: `internal/store/models.go`
- Create: `internal/store/schema.go`
- Create: `internal/store/store.go`
- Test: `internal/store/store_test.go`

**Interfaces:**
- Produces: `Request`, `SyncState`, `PricingModel`, `OverviewStats` structs in `package store`.
- Produces: `type Store struct`, `NewStore(dbPath string) (*Store, error)`, `(s *Store) Close() error`.
- Produces: `(s *Store) InsertRequests(reqs []Request) (int, error)`.
- Produces: `(s *Store) GetSyncState(sourcePath string) (*SyncState, error)`, `(s *Store) SetSyncState(state SyncState) error`.
- Produces: `(s *Store) GetOverview(period string) (*OverviewStats, error)`.

- [ ] **Step 1: Write failing test for Store creation, schema initialization, deduplication, and sync state**

Create `internal/store/store_test.go`:
```go
package store_test

import (
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestStore_InitAndDeduplication(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatalf("failed to init store: %v", err)
	}
	defer st.Close()

	now := time.Now().UTC().Truncate(time.Second)
	req1 := store.Request{
		ID:           "req-1",
		Tool:         "claude_code",
		SessionID:    "sess-1",
		Timestamp:    now,
		Model:        "claude-opus-5",
		InputTokens:  100,
		OutputTokens: 50,
		TotalTokens:  150,
		CostUSD:      pointerFloat(0.02),
		LatencyMS:    pointerInt(1200),
		Status:       "success",
	}

	inserted, err := st.InsertRequests([]store.Request{req1})
	if err != nil {
		t.Fatalf("failed to insert: %v", err)
	}
	if inserted != 1 {
		t.Fatalf("expected 1 inserted, got %d", inserted)
	}

	// Re-insert exact same request (dedup test)
	inserted2, err := st.InsertRequests([]store.Request{req1})
	if err != nil {
		t.Fatalf("failed to re-insert: %v", err)
	}
	if inserted2 != 0 {
		t.Fatalf("expected 0 inserted on duplicate, got %d", inserted2)
	}
}

func TestStore_SyncState(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	state, err := st.GetSyncState("/path/to/log.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if state.LastOffset != 0 {
		t.Fatalf("expected 0 offset for new state, got %d", state.LastOffset)
	}

	state.LastOffset = 1024
	state.Tool = "claude_code"
	if err := st.SetSyncState(*state); err != nil {
		t.Fatal(err)
	}

	retrieved, err := st.GetSyncState("/path/to/log.jsonl")
	if err != nil {
		t.Fatal(err)
	}
	if retrieved.LastOffset != 1024 {
		t.Fatalf("expected 1024 offset, got %d", retrieved.LastOffset)
	}
}

func pointerFloat(v float64) *float64 { return &v }
func pointerInt(v int64) *int64       { return &v }
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/store/... -v`
Expected: FAIL (types and package missing)

- [ ] **Step 3: Implement models, schema, and store**

Create `internal/store/models.go`:
```go
package store

import "time"

type Request struct {
	ID               string    `json:"id"`
	Tool             string    `json:"tool"`
	SessionID        string    `json:"session_id"`
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CostUSD          *float64  `json:"cost_usd"`
	LatencyMS        *int64    `json:"latency_ms"`
	Status           string    `json:"status"`
	PromptText       string    `json:"prompt_text,omitempty"`
	ResponseText     string    `json:"response_text,omitempty"`
	SourceFile       string    `json:"source_file"`
	RawData          string    `json:"raw_data,omitempty"`
	ImportedAt       time.Time `json:"imported_at"`
}

type SyncState struct {
	SourcePath   string     `json:"source_path"`
	Tool         string     `json:"tool"`
	LastOffset   int64      `json:"last_offset"`
	LastModified *time.Time `json:"last_modified"`
	LastRowID    string     `json:"last_row_id"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type OverviewStats struct {
	TotalRequests int64    `json:"total_requests"`
	TotalTokens   int64    `json:"total_tokens"`
	TotalCost     float64  `json:"total_cost"`
	AvgLatencyMS  float64  `json:"avg_latency_ms"`
}
```

Create `internal/store/schema.go`:
```go
package store

const SchemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS requests (
    id                 TEXT PRIMARY KEY,
    tool               TEXT NOT NULL,
    session_id         TEXT,
    timestamp          DATETIME NOT NULL,
    model              TEXT,
    input_tokens       INTEGER DEFAULT 0,
    output_tokens      INTEGER DEFAULT 0,
    cache_read_tokens  INTEGER DEFAULT 0,
    cache_write_tokens INTEGER DEFAULT 0,
    total_tokens       INTEGER DEFAULT 0,
    cost_usd           REAL,
    latency_ms         INTEGER,
    status             TEXT DEFAULT 'success',
    prompt_text        TEXT,
    response_text      TEXT,
    source_file        TEXT,
    raw_data           TEXT,
    imported_at        DATETIME DEFAULT CURRENT_TIMESTAMP,
    UNIQUE(tool, session_id, timestamp, model)
);

CREATE TABLE IF NOT EXISTS pricing (
    model_pattern       TEXT PRIMARY KEY,
    input_per_mtok      REAL NOT NULL,
    output_per_mtok     REAL NOT NULL,
    cache_read_per_mtok REAL DEFAULT 0,
    cache_write_per_mtok REAL DEFAULT 0,
    effective_date      DATE NOT NULL,
    source              TEXT
);

CREATE TABLE IF NOT EXISTS sync_state (
    source_path    TEXT PRIMARY KEY,
    tool           TEXT NOT NULL,
    last_offset    INTEGER DEFAULT 0,
    last_modified  DATETIME,
    last_row_id    TEXT,
    updated_at     DATETIME DEFAULT CURRENT_TIMESTAMP
);

CREATE INDEX IF NOT EXISTS idx_requests_timestamp ON requests(timestamp);
CREATE INDEX IF NOT EXISTS idx_requests_tool ON requests(tool);
CREATE INDEX IF NOT EXISTS idx_requests_model ON requests(model);
CREATE INDEX IF NOT EXISTS idx_requests_tool_timestamp ON requests(tool, timestamp);
`
```

Create `internal/store/store.go`:
```go
package store

import (
	"database/sql"
	"fmt"
	"os"
	"path/filepath"
	"time"

	_ "github.com/mattn/go-sqlite3"
)

type Store struct {
	db *sql.DB
}

func NewStore(dbPath string) (*Store, error) {
	if err := os.MkdirAll(filepath.Dir(dbPath), 0755); err != nil {
		return nil, fmt.Errorf("failed to create db dir: %w", err)
	}

	db, err := sql.Open("sqlite3", dbPath+"?_journal=WAL&_busy_timeout=5000")
	if err != nil {
		return nil, fmt.Errorf("failed to open sqlite db: %w", err)
	}

	if _, err := db.Exec(SchemaSQL); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to execute schema: %w", err)
	}

	return &Store{db: db}, nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) InsertRequests(reqs []Request) (int, error) {
	if len(reqs) == 0 {
		return 0, nil
	}

	tx, err := s.db.Begin()
	if err != nil {
		return 0, err
	}
	defer tx.Rollback()

	stmt, err := tx.Prepare(`
		INSERT OR IGNORE INTO requests (
			id, tool, session_id, timestamp, model,
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens,
			cost_usd, latency_ms, status, prompt_text, response_text,
			source_file, raw_data
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var inserted int
	for _, r := range reqs {
		res, err := stmt.Exec(
			r.ID, r.Tool, r.SessionID, r.Timestamp.Format(time.RFC3339), r.Model,
			r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.TotalTokens,
			r.CostUSD, r.LatencyMS, r.Status, r.PromptText, r.ResponseText,
			r.SourceFile, r.RawData,
		)
		if err != nil {
			return 0, err
		}
		rows, _ := res.RowsAffected()
		if rows > 0 {
			inserted++
		}
	}

	if err := tx.Commit(); err != nil {
		return 0, err
	}
	return inserted, nil
}

func (s *Store) GetSyncState(sourcePath string) (*SyncState, error) {
	row := s.db.QueryRow(`
		SELECT source_path, tool, last_offset, last_modified, last_row_id, updated_at
		FROM sync_state WHERE source_path = ?
	`, sourcePath)

	var st SyncState
	var lastMod sql.NullTime
	var lastRow sql.NullString
	err := row.Scan(&st.SourcePath, &st.Tool, &st.LastOffset, &lastMod, &lastRow, &st.UpdatedAt)
	if err == sql.ErrNoRows {
		return &SyncState{
			SourcePath: sourcePath,
			LastOffset: 0,
		}, nil
	}
	if err != nil {
		return nil, err
	}

	if lastMod.Valid {
		st.LastModified = &lastMod.Time
	}
	if lastRow.Valid {
		st.LastRowID = lastRow.String
	}
	return &st, nil
}

func (s *Store) SetSyncState(state SyncState) error {
	_, err := s.db.Exec(`
		INSERT INTO sync_state (source_path, tool, last_offset, last_modified, last_row_id, updated_at)
		VALUES (?, ?, ?, ?, ?, CURRENT_TIMESTAMP)
		ON CONFLICT(source_path) DO UPDATE SET
			tool = excluded.tool,
			last_offset = excluded.last_offset,
			last_modified = excluded.last_modified,
			last_row_id = excluded.last_row_id,
			updated_at = CURRENT_TIMESTAMP
	`, state.SourcePath, state.Tool, state.LastOffset, state.LastModified, state.LastRowID)
	return err
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/store/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/store/
git commit -m "feat(store): implement SQLite database schema, sync state, and deduplication"
```

---

### Task 3: Pricing Engine & Cost Calculation

**Files:**
- Create: `internal/pricing/models.go`
- Create: `internal/pricing/defaults.go`
- Create: `internal/pricing/engine.go`
- Test: `internal/pricing/engine_test.go`

**Interfaces:**
- Produces: `type ModelPrice struct`, `type Engine struct`.
- Produces: `NewEngine(customPrices map[string]ModelPrice) *Engine`.
- Produces: `(e *Engine) CalculateCost(model string, inputTokens, outputTokens, cacheReadTokens, cacheWriteTokens int64) (*float64, bool)`.

- [ ] **Step 1: Write failing test for PricingEngine pattern matching and calculations**

Create `internal/pricing/engine_test.go`:
```go
package pricing_test

import (
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
)

func TestPricingEngine_StandardModels(t *testing.T) {
	e := pricing.NewEngine(nil)

	// Claude Opus: Input $15/m, Output $75/m
	cost, ok := e.CalculateCost("claude-opus-5", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected model to match")
	}
	// 15 + (0.1 * 75) = 15 + 7.5 = 22.5
	expected := 22.50
	if cost == nil || *cost < 22.49 || *cost > 22.51 {
		t.Fatalf("expected cost ~22.50, got %v", cost)
	}

	// Unknown model
	unknownCost, ok := e.CalculateCost("totally-unknown-model-xyz", 1000, 1000, 0, 0)
	if ok || unknownCost != nil {
		t.Fatalf("expected unknown model to return false and nil cost, got %v, %v", ok, unknownCost)
	}
}

func TestPricingEngine_CacheTokens(t *testing.T) {
	e := pricing.NewEngine(nil)

	// Test cache read discount
	cost, ok := e.CalculateCost("claude-3-5-sonnet", 100_000, 10_000, 500_000, 0)
	if !ok || cost == nil {
		t.Fatal("expected valid calculation")
	}
	if *cost <= 0 {
		t.Fatal("cost should be positive")
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/pricing/... -v`
Expected: FAIL (missing package)

- [ ] **Step 3: Implement pricing models, default rates, and engine**

Create `internal/pricing/models.go`:
```go
package pricing

type ModelPrice struct {
	InputPerMTok      float64 `json:"input_per_mtok"`
	OutputPerMTok     float64 `json:"output_per_mtok"`
	CacheReadPerMTok  float64 `json:"cache_read_per_mtok"`
	CacheWritePerMTok float64 `json:"cache_write_per_mtok"`
}
```

Create `internal/pricing/defaults.go`:
```go
package pricing

// DefaultModelPrices contains fallback token pricing per million tokens
var DefaultModelPrices = map[string]ModelPrice{
	"claude-opus*": {
		InputPerMTok:      15.00,
		OutputPerMTok:     75.00,
		CacheReadPerMTok:  1.50,
		CacheWritePerMTok: 18.75,
	},
	"claude-3-5-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-3-7-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-3-5-haiku*": {
		InputPerMTok:      0.80,
		OutputPerMTok:     4.00,
		CacheReadPerMTok:  0.08,
		CacheWritePerMTok: 1.00,
	},
	"gpt-4o": {
		InputPerMTok:  2.50,
		OutputPerMTok: 10.00,
	},
	"gpt-4o-mini": {
		InputPerMTok:  0.15,
		OutputPerMTok: 0.60,
	},
	"gemini-2*flash*": {
		InputPerMTok:  0.10,
		OutputPerMTok: 0.40,
	},
	"gemini-2*pro*": {
		InputPerMTok:  1.25,
		OutputPerMTok: 5.00,
	},
}
```

Create `internal/pricing/engine.go`:
```go
package pricing

import (
	"path/filepath"
	"strings"
)

type Engine struct {
	prices map[string]ModelPrice
}

func NewEngine(customPrices map[string]ModelPrice) *Engine {
	merged := make(map[string]ModelPrice)
	for k, v := range DefaultModelPrices {
		merged[k] = v
	}
	for k, v := range customPrices {
		merged[k] = v
	}
	return &Engine{prices: merged}
}

func (e *Engine) CalculateCost(model string, input, output, cacheRead, cacheWrite int64) (*float64, bool) {
	if model == "" {
		return nil, false
	}

	modelLower := strings.ToLower(model)
	var price ModelPrice
	var matched bool

	for pattern, p := range e.prices {
		match, err := filepath.Match(strings.ToLower(pattern), modelLower)
		if err == nil && match {
			price = p
			matched = true
			break
		}
		// Also support contains check if no wildcard
		if !strings.Contains(pattern, "*") && strings.Contains(modelLower, strings.ToLower(pattern)) {
			price = p
			matched = true
			break
		}
	}

	if !matched {
		return nil, false
	}

	cost := (float64(input)/1_000_000.0)*price.InputPerMTok +
		(float64(output)/1_000_000.0)*price.OutputPerMTok +
		(float64(cacheRead)/1_000_000.0)*price.CacheReadPerMTok +
		(float64(cacheWrite)/1_000_000.0)*price.CacheWritePerMTok

	return &cost, true
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/pricing/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/pricing/
git commit -m "feat(pricing): add token cost calculator with default model pricing"
```

---

### Task 4: Parser Architecture & Claude Code Parser

**Files:**
- Create: `internal/parsers/parser.go`
- Create: `internal/parsers/registry.go`
- Create: `internal/parsers/claude_code.go`
- Create: `internal/parsers/testdata/claude_sample.jsonl`
- Test: `internal/parsers/claude_code_test.go`

**Interfaces:**
- Produces: `Parser` interface, `DataSource` struct in `package parsers`.
- Produces: `ClaudeCodeParser` struct implementing `Parser`.
- Produces: `Register(p Parser)`, `GetParser(tool string) (Parser, bool)`.

- [ ] **Step 1: Create sample fixture and write failing test for ClaudeCodeParser**

Create `internal/parsers/testdata/claude_sample.jsonl`:
```json
{"type":"user","message":{"role":"user","content":"hello"}}
{"type":"assistant","costUSD":0.0125,"durationMs":1540,"message":{"model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":1200,"output_tokens":85,"cache_read_input_tokens":300,"cache_creation_input_tokens":0}}}
{"type":"assistant","costUSD":0.0210,"durationMs":2100,"message":{"model":"claude-3-7-sonnet","role":"assistant","usage":{"input_tokens":2100,"output_tokens":120,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}
```

Create `internal/parsers/claude_code_test.go`:
```go
package parsers_test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestClaudeCodeParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/claude_sample.jsonl")
	parser := &parsers.ClaudeCodeParser{}

	state := store.SyncState{
		SourcePath: fixturePath,
		Tool:       "claude_code",
		LastOffset: 0,
	}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}

	if len(reqs) != 2 {
		t.Fatalf("expected 2 assistant turns, got %d", len(reqs))
	}

	first := reqs[0]
	if first.Tool != "claude_code" {
		t.Errorf("expected tool claude_code, got %s", first.Tool)
	}
	if first.Model != "claude-3-7-sonnet" {
		t.Errorf("expected claude-3-7-sonnet, got %s", first.Model)
	}
	if first.InputTokens != 1200 || first.OutputTokens != 85 {
		t.Errorf("tokens mismatch: in=%d, out=%d", first.InputTokens, first.OutputTokens)
	}
	if first.CostUSD == nil || *first.CostUSD != 0.0125 {
		t.Errorf("cost mismatch: %v", first.CostUSD)
	}
	if first.CacheReadTokens != 300 {
		t.Errorf("cache read mismatch: %d", first.CacheReadTokens)
	}
	if newState.LastOffset <= 0 {
		t.Errorf("expected positive offset advance, got %d", newState.LastOffset)
	}

	// Incremental test from the new offset
	reqs2, _, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, newState)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs2) != 0 {
		t.Fatalf("expected 0 new requests from end of file, got %d", len(reqs2))
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/parsers/... -v`
Expected: FAIL

- [ ] **Step 3: Implement parser interface, registry, and Claude Code parser**

Create `internal/parsers/parser.go`:
```go
package parsers

import (
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type DataSource struct {
	Path string
	Type string // "jsonl" or "sqlite"
}

type Parser interface {
	Name() string
	Discover(cfg config.ToolConfig) ([]DataSource, error)
	Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error)
}
```

Create `internal/parsers/registry.go`:
```go
package parsers

import "sync"

var (
	registryLock sync.RWMutex
	registry     = make(map[string]Parser)
)

func Register(p Parser) {
	registryLock.Lock()
	defer registryLock.Unlock()
	registry[p.Name()] = p
}

func Get(name string) (Parser, bool) {
	registryLock.RLock()
	defer registryLock.RUnlock()
	p, ok := registry[name]
	return p, ok
}

func All() []Parser {
	registryLock.RLock()
	defer registryLock.RUnlock()
	list := make([]Parser, 0, len(registry))
	for _, p := range registry {
		list = append(list, p)
	}
	return list
}
```

Create `internal/parsers/claude_code.go`:
```go
package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type ClaudeCodeParser struct{}

func init() {
	Register(&ClaudeCodeParser{})
}

func (p *ClaudeCodeParser) Name() string {
	return "claude_code"
}

func (p *ClaudeCodeParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if strings.HasSuffix(path, ".jsonl") {
				sources = append(sources, DataSource{Path: path, Type: "jsonl"})
			}
			return nil
		})
	}
	return sources, nil
}

type claudeAssistantEvent struct {
	Type       string   `json:"type"`
	CostUSD    *float64 `json:"costUSD"`
	DurationMS *int64   `json:"durationMs"`
	Message    struct {
		Model string `json:"model"`
		Usage struct {
			InputTokens             int64 `json:"input_tokens"`
			OutputTokens            int64 `json:"output_tokens"`
			CacheReadInputTokens    int64 `json:"cache_read_input_tokens"`
			CacheCreationInputTokens int64 `json:"cache_creation_input_tokens"`
		} `json:"usage"`
	} `json:"message"`
}

func (p *ClaudeCodeParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, state, err
	}
	defer file.Close()

	if state.LastOffset > 0 {
		if _, err := file.Seek(state.LastOffset, io.SeekStart); err != nil {
			state.LastOffset = 0
			file.Seek(0, io.SeekStart)
		}
	}

	reader := bufio.NewReader(file)
	var requests []store.Request
	currentOffset := state.LastOffset
	sessionID := strings.TrimSuffix(filepath.Base(source.Path), ".jsonl")

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			currentOffset += int64(len(line))
			var evt claudeAssistantEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Type == "assistant" {
				req := store.Request{
					ID:               uuid.New().String(),
					Tool:             "claude_code",
					SessionID:        sessionID,
					Timestamp:        time.Now().UTC(),
					Model:            evt.Message.Model,
					InputTokens:      evt.Message.Usage.InputTokens,
					OutputTokens:     evt.Message.Usage.OutputTokens,
					CacheReadTokens:  evt.Message.Usage.CacheReadInputTokens,
					CacheWriteTokens: evt.Message.Usage.CacheCreationInputTokens,
					TotalTokens:      evt.Message.Usage.InputTokens + evt.Message.Usage.OutputTokens,
					CostUSD:          evt.CostUSD,
					LatencyMS:        evt.DurationMS,
					Status:           "success",
					SourceFile:       source.Path,
				}
				requests = append(requests, req)
			}
		}
		if err != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	return requests, state, nil
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/parsers/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/
git commit -m "feat(parsers): implement parser interface, registry, and Claude Code log parser"
```

---

### Task 5: AGY (Antigravity) and Aider Parsers

**Files:**
- Create: `internal/parsers/agy.go`
- Create: `internal/parsers/aider.go`
- Create: `internal/parsers/testdata/agy_sample.jsonl`
- Create: `internal/parsers/testdata/aider_analytics.jsonl`
- Test: `internal/parsers/agy_test.go`
- Test: `internal/parsers/aider_test.go`

**Interfaces:**
- Produces: `AGYParser` in `internal/parsers/agy.go`
- Produces: `AiderParser` in `internal/parsers/aider.go`

- [ ] **Step 1: Create sample fixtures for AGY and Aider**

Create `internal/parsers/testdata/agy_sample.jsonl`:
```json
{"step_index":0,"source":"USER_EXPLICIT","type":"USER_INPUT","status":"DONE","created_at":"2026-10-17T12:00:00Z","content":"fix this bug"}
{"step_index":1,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:05Z","input_tokens":15420,"output_tokens":850,"cache_read_tokens":1200}
```

Create `internal/parsers/testdata/aider_analytics.jsonl`:
```json
{"event":"message_send","properties":{"main_model":"anthropic/claude-3-5-sonnet-20241022","prompt_tokens":114112,"completion_tokens":94,"total_tokens":114206,"cost":0.3423},"time":1742560429}
```

- [ ] **Step 2: Write failing tests for AGY and Aider parsers**

Create `internal/parsers/agy_test.go`:
```go
package parsers_test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestAGYParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/agy_sample.jsonl")
	parser := &parsers.AGYParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "agy"}
	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 PLANNER_RESPONSE turn, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "agy" {
		t.Errorf("expected agy, got %s", req.Tool)
	}
	if req.InputTokens != 15420 || req.OutputTokens != 850 {
		t.Errorf("token count mismatch: in=%d, out=%d", req.InputTokens, req.OutputTokens)
	}
	if req.CacheReadTokens != 1200 {
		t.Errorf("cache read tokens mismatch: %d", req.CacheReadTokens)
	}
	if newState.LastOffset <= 0 {
		t.Errorf("offset did not advance: %d", newState.LastOffset)
	}
}
```

Create `internal/parsers/aider_test.go`:
```go
package parsers_test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestAiderParser_Parse(t *testing.T) {
	fixturePath, _ := filepath.Abs("testdata/aider_analytics.jsonl")
	parser := &parsers.AiderParser{}

	state := store.SyncState{SourcePath: fixturePath, Tool: "aider"}
	reqs, newState, err := parser.Parse(parsers.DataSource{Path: fixturePath, Type: "jsonl"}, state)
	if err != nil {
		t.Fatal(err)
	}

	if len(reqs) != 1 {
		t.Fatalf("expected 1 message_send event, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "aider" {
		t.Errorf("expected aider, got %s", req.Tool)
	}
	if req.Model != "anthropic/claude-3-5-sonnet-20241022" {
		t.Errorf("expected claude-3-5-sonnet model, got %s", req.Model)
	}
	if req.CostUSD == nil || *req.CostUSD != 0.3423 {
		t.Errorf("expected cost 0.3423, got %v", req.CostUSD)
	}
	if newState.LastOffset <= 0 {
		t.Errorf("offset did not advance: %d", newState.LastOffset)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/parsers/... -v`
Expected: FAIL (missing implementations)

- [ ] **Step 4: Implement AGY and Aider parsers**

Create `internal/parsers/agy.go`:
```go
package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"

	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type AGYParser struct{}

func init() {
	Register(&AGYParser{})
}

func (p *AGYParser) Name() string {
	return "agy"
}

func (p *AGYParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, searchPath := range cfg.Paths {
		expanded := config.ExpandHome(searchPath)
		_ = filepath.Walk(expanded, func(path string, info os.FileInfo, err error) error {
			if err != nil || info == nil || info.IsDir() {
				return nil
			}
			if filepath.Base(path) == "transcript.jsonl" {
				sources = append(sources, DataSource{Path: path, Type: "jsonl"})
			}
			return nil
		})
	}
	return sources, nil
}

type agyTranscriptEvent struct {
	Source          string `json:"source"`
	Type            string `json:"type"`
	Status          string `json:"status"`
	CreatedAt       string `json:"created_at"`
	InputTokens     int64  `json:"input_tokens"`
	OutputTokens    int64  `json:"output_tokens"`
	CacheReadTokens int64  `json:"cache_read_tokens"`
}

func (p *AGYParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, state, err
	}
	defer file.Close()

	if state.LastOffset > 0 {
		if _, err := file.Seek(state.LastOffset, io.SeekStart); err != nil {
			state.LastOffset = 0
			file.Seek(0, io.SeekStart)
		}
	}

	reader := bufio.NewReader(file)
	var requests []store.Request
	currentOffset := state.LastOffset

	// Extract conversation id from path: .../brain/<id>/.system_generated/logs/transcript.jsonl
	parts := strings.Split(source.Path, string(filepath.Separator))
	sessionID := "agy-session"
	for i, part := range parts {
		if part == "brain" && i+1 < len(parts) {
			sessionID = parts[i+1]
			break
		}
	}

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			currentOffset += int64(len(line))
			var evt agyTranscriptEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Type == "PLANNER_RESPONSE" {
				ts := time.Now().UTC()
				if parsed, tErr := time.Parse(time.RFC3339, evt.CreatedAt); tErr == nil {
					ts = parsed
				}
				req := store.Request{
					ID:              uuid.New().String(),
					Tool:            "agy",
					SessionID:       sessionID,
					Timestamp:       ts,
					Model:           "gemini-2-pro", // Conversation-level default fallback
					InputTokens:     evt.InputTokens,
					OutputTokens:    evt.OutputTokens,
					CacheReadTokens: evt.CacheReadTokens,
					TotalTokens:     evt.InputTokens + evt.OutputTokens,
					Status:          "success",
					SourceFile:      source.Path,
				}
				requests = append(requests, req)
			}
		}
		if err != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	return requests, state, nil
}
```

Create `internal/parsers/aider.go`:
```go
package parsers

import (
	"bufio"
	"encoding/json"
	"io"
	"os"
	"path/filepath"
	"time"

	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type AiderParser struct{}

func init() {
	Register(&AiderParser{})
}

func (p *AiderParser) Name() string {
	return "aider"
}

func (p *AiderParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	if cfg.AnalyticsLog != "" {
		expanded := config.ExpandHome(cfg.AnalyticsLog)
		if _, err := os.Stat(expanded); err == nil {
			sources = append(sources, DataSource{Path: expanded, Type: "jsonl"})
		}
	}
	return sources, nil
}

type aiderAnalyticsEvent struct {
	Event      string `json:"event"`
	Properties struct {
		MainModel        string   `json:"main_model"`
		PromptTokens     int64    `json:"prompt_tokens"`
		CompletionTokens int64    `json:"completion_tokens"`
		TotalTokens      int64    `json:"total_tokens"`
		Cost             *float64 `json:"cost"`
	} `json:"properties"`
	Time int64 `json:"time"`
}

func (p *AiderParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	file, err := os.Open(source.Path)
	if err != nil {
		return nil, state, err
	}
	defer file.Close()

	if state.LastOffset > 0 {
		if _, err := file.Seek(state.LastOffset, io.SeekStart); err != nil {
			state.LastOffset = 0
			file.Seek(0, io.SeekStart)
		}
	}

	reader := bufio.NewReader(file)
	var requests []store.Request
	currentOffset := state.LastOffset

	for {
		line, err := reader.ReadBytes('\n')
		if len(line) > 0 {
			currentOffset += int64(len(line))
			var evt aiderAnalyticsEvent
			if jsonErr := json.Unmarshal(line, &evt); jsonErr == nil && evt.Event == "message_send" {
				ts := time.Now().UTC()
				if evt.Time > 0 {
					ts = time.Unix(evt.Time, 0).UTC()
				}
				req := store.Request{
					ID:           uuid.New().String(),
					Tool:         "aider",
					SessionID:    filepath.Base(source.Path),
					Timestamp:    ts,
					Model:        evt.Properties.MainModel,
					InputTokens:  evt.Properties.PromptTokens,
					OutputTokens: evt.Properties.CompletionTokens,
					TotalTokens:  evt.Properties.TotalTokens,
					CostUSD:      evt.Properties.Cost,
					Status:       "success",
					SourceFile:   source.Path,
				}
				requests = append(requests, req)
			}
		}
		if err != nil {
			break
		}
	}

	state.LastOffset = currentOffset
	return requests, state, nil
}
```

- [ ] **Step 5: Run tests and verify they pass**

Run: `go test ./internal/parsers/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/parsers/
git commit -m "feat(parsers): add AGY transcript parser and Aider analytics log parser"
```

---

### Task 6: Codex CLI & OpenCode SQLite Parsers

**Files:**
- Create: `internal/parsers/codex.go`
- Create: `internal/parsers/opencode.go`
- Test: `internal/parsers/codex_test.go`

**Interfaces:**
- Produces: `CodexParser` in `internal/parsers/codex.go`
- Produces: `OpenCodeParser` in `internal/parsers/opencode.go`

- [ ] **Step 1: Write failing test for Codex SQLite parser using temporary SQLite DB**

Create `internal/parsers/codex_test.go`:
```go
package parsers_test

import (
	"database/sql"
	"path/filepath"
	"testing"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestCodexParser_Parse(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "state_5.sqlite")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE threads (
			id TEXT PRIMARY KEY,
			model_provider TEXT,
			tokens_used INTEGER,
			created_at DATETIME,
			updated_at DATETIME
		);
		INSERT INTO threads (id, model_provider, tokens_used, created_at, updated_at)
		VALUES ('thread-1', 'openai/gpt-4o', 4500, '2026-10-17 10:00:00', '2026-10-17 10:05:00');
	`)
	if err != nil {
		t.Fatal(err)
	}

	parser := &parsers.CodexParser{}
	state := store.SyncState{SourcePath: dbPath, Tool: "codex"}

	reqs, newState, err := parser.Parse(parsers.DataSource{Path: dbPath, Type: "sqlite"}, state)
	if err != nil {
		t.Fatal(err)
	}
	if len(reqs) != 1 {
		t.Fatalf("expected 1 thread request, got %d", len(reqs))
	}
	req := reqs[0]
	if req.Tool != "codex" {
		t.Errorf("expected codex, got %s", req.Tool)
	}
	if req.TotalTokens != 4500 {
		t.Errorf("expected 4500 tokens, got %d", req.TotalTokens)
	}
	if req.Model != "openai/gpt-4o" {
		t.Errorf("expected openai/gpt-4o model, got %s", req.Model)
	}
	if newState.LastRowID != "thread-1" {
		t.Errorf("expected LastRowID to update, got %s", newState.LastRowID)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/parsers/codex_test.go -v`
Expected: FAIL

- [ ] **Step 3: Implement Codex and OpenCode parsers**

Create `internal/parsers/codex.go`:
```go
package parsers

import (
	"database/sql"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type CodexParser struct{}

func init() {
	Register(&CodexParser{})
}

func (p *CodexParser) Name() string {
	return "codex"
}

func (p *CodexParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, path := range cfg.Paths {
		expanded := config.ExpandHome(path)
		if _, err := os.Stat(expanded); err == nil {
			sources = append(sources, DataSource{Path: expanded, Type: "sqlite"})
		}
	}
	return sources, nil
}

func (p *CodexParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	db, err := sql.Open("sqlite3", source.Path+"?mode=ro")
	if err != nil {
		return nil, state, err
	}
	defer db.Close()

	query := `
		SELECT id, model_provider, tokens_used, created_at
		FROM threads
	`
	var rows *sql.Rows
	if state.LastModified != nil {
		query += " WHERE created_at > ? ORDER BY created_at ASC"
		rows, err = db.Query(query, state.LastModified)
	} else {
		query += " ORDER BY created_at ASC"
		rows, err = db.Query(query)
	}
	if err != nil {
		return nil, state, err
	}
	defer rows.Close()

	var requests []store.Request
	var lastID string
	var lastTime *time.Time

	for rows.Next() {
		var id string
		var model sql.NullString
		var tokens sql.NullInt64
		var createdAt sql.NullTime

		if err := rows.Scan(&id, &model, &tokens, &createdAt); err != nil {
			continue
		}

		ts := time.Now().UTC()
		if createdAt.Valid {
			ts = createdAt.Time
			lastTime = &ts
		}
		lastID = id

		modelStr := "gpt-4o"
		if model.Valid && model.String != "" {
			modelStr = model.String
		}

		tokVal := int64(0)
		if tokens.Valid {
			tokVal = tokens.Int64
		}

		req := store.Request{
			ID:          uuid.New().String(),
			Tool:        "codex",
			SessionID:   id,
			Timestamp:   ts,
			Model:       modelStr,
			TotalTokens: tokVal,
			InputTokens: tokVal / 2, // Estimated split when not reported
			OutputTokens: tokVal / 2,
			Status:      "success",
			SourceFile:  source.Path,
		}
		requests = append(requests, req)
	}

	state.LastRowID = lastID
	if lastTime != nil {
		state.LastModified = lastTime
	}
	return requests, state, nil
}
```

Create `internal/parsers/opencode.go`:
```go
package parsers

import (
	"database/sql"
	"os"
	"time"

	_ "github.com/mattn/go-sqlite3"
	"github.com/google/uuid"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type OpenCodeParser struct{}

func init() {
	Register(&OpenCodeParser{})
}

func (p *OpenCodeParser) Name() string {
	return "opencode"
}

func (p *OpenCodeParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	var sources []DataSource
	for _, path := range cfg.Paths {
		expanded := config.ExpandHome(path)
		if _, err := os.Stat(expanded); err == nil {
			sources = append(sources, DataSource{Path: expanded, Type: "sqlite"})
		}
	}
	return sources, nil
}

func (p *OpenCodeParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	db, err := sql.Open("sqlite3", source.Path+"?mode=ro")
	if err != nil {
		return nil, state, err
	}
	defer db.Close()

	// Defensive check: does messages table exist?
	var tblExists int
	_ = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='messages'").Scan(&tblExists)
	if tblExists == 0 {
		return nil, state, nil
	}

	rows, err := db.Query(`
		SELECT id, session_id, model, tokens_in, tokens_out, created_at
		FROM messages
		ORDER BY created_at ASC
	`)
	if err != nil {
		return nil, state, nil // Table structure may differ across versions; fail safe
	}
	defer rows.Close()

	var requests []store.Request
	for rows.Next() {
		var id, sessionID, model string
		var tokensIn, tokensOut int64
		var createdAt sql.NullTime

		if err := rows.Scan(&id, &sessionID, &model, &tokensIn, &tokensOut, &createdAt); err != nil {
			continue
		}

		ts := time.Now().UTC()
		if createdAt.Valid {
			ts = createdAt.Time
		}

		req := store.Request{
			ID:           uuid.New().String(),
			Tool:         "opencode",
			SessionID:    sessionID,
			Timestamp:    ts,
			Model:        model,
			InputTokens:  tokensIn,
			OutputTokens: tokensOut,
			TotalTokens:  tokensIn + tokensOut,
			Status:       "success",
			SourceFile:   source.Path,
		}
		requests = append(requests, req)
	}

	return requests, state, nil
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/parsers/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/parsers/
git commit -m "feat(parsers): implement Codex and OpenCode SQLite database parsers"
```

---

### Task 7: Synchronization Engine & Daemon Service

**Files:**
- Create: `internal/daemon/sync.go`
- Create: `internal/daemon/daemon.go`
- Test: `internal/daemon/sync_test.go`

**Interfaces:**
- Produces: `func SyncAll(cfg *config.Config, st *store.Store, engine *pricing.Engine) (int, error)`
- Produces: `type Daemon struct`, `NewDaemon(cfg *config.Config, st *store.Store, engine *pricing.Engine) *Daemon`
- Produces: `(d *Daemon) Start(ctx context.Context) error`
- Produces: `func StartBackgroundProcess() error`, `func StopBackgroundProcess() error`

- [ ] **Step 1: Write test for SyncAll coordinating parsers, store, and pricing engine**

Create `internal/daemon/sync_test.go`:
```go
package daemon_test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestSyncAll_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	// Set config pointing claude_code to testdata fixture
	fixtureDir, _ := filepath.Abs("../parsers/testdata")
	cfg := config.DefaultConfig()
	cfg.Tools["claude_code"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{fixtureDir},
	}
	cfg.Tools["agy"] = config.ToolConfig{Enabled: false}
	cfg.Tools["aider"] = config.ToolConfig{Enabled: false}
	cfg.Tools["codex"] = config.ToolConfig{Enabled: false}
	cfg.Tools["opencode"] = config.ToolConfig{Enabled: false}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported records from sample, got %d", imported)
	}

	// Re-sync should import 0 because offset advanced
	importedAgain, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if importedAgain != 0 {
		t.Fatalf("expected 0 new records on second sync, got %d", importedAgain)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/daemon/... -v`
Expected: FAIL

- [ ] **Step 3: Implement SyncAll and Daemon lifecycle**

Create `internal/daemon/sync.go`:
```go
package daemon

import (
	"log/slog"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func SyncAll(cfg *config.Config, st *store.Store, engine *pricing.Engine) (int, error) {
	totalImported := 0

	for toolName, toolCfg := range cfg.Tools {
		if !toolCfg.Enabled {
			continue
		}

		p, ok := parsers.Get(toolName)
		if !ok {
			slog.Debug("parser not found for tool", "tool", toolName)
			continue
		}

		sources, err := p.Discover(toolCfg)
		if err != nil {
			slog.Warn("failed to discover sources for tool", "tool", toolName, "err", err)
			continue
		}

		for _, src := range sources {
			state, err := st.GetSyncState(src.Path)
			if err != nil {
				slog.Warn("failed to get sync state", "file", src.Path, "err", err)
				continue
			}
			state.Tool = toolName

			reqs, newState, err := p.Parse(src, *state)
			if err != nil {
				slog.Warn("failed to parse source", "file", src.Path, "err", err)
				continue
			}

			// If tool did not supply cost, calculate it using pricing engine
			for i := range reqs {
				if reqs[i].CostUSD == nil {
					cost, ok := engine.CalculateCost(
						reqs[i].Model,
						reqs[i].InputTokens,
						reqs[i].OutputTokens,
						reqs[i].CacheReadTokens,
						reqs[i].CacheWriteTokens,
					)
					if ok {
						reqs[i].CostUSD = cost
					}
				}
			}

			inserted, err := st.InsertRequests(reqs)
			if err != nil {
				slog.Error("failed to insert requests", "file", src.Path, "err", err)
				continue
			}
			totalImported += inserted

			if err := st.SetSyncState(newState); err != nil {
				slog.Warn("failed to update sync state", "file", src.Path, "err", err)
			}
		}
	}

	return totalImported, nil
}
```

Create `internal/daemon/daemon.go`:
```go
package daemon

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type Daemon struct {
	cfg    *config.Config
	store  *store.Store
	engine *pricing.Engine
}

func NewDaemon(cfg *config.Config, st *store.Store, engine *pricing.Engine) *Daemon {
	return &Daemon{
		cfg:    cfg,
		store:  st,
		engine: engine,
	}
}

func (d *Daemon) PIDFilePath() string {
	return filepath.Join(d.cfg.DataDir, "daemon.pid")
}

func (d *Daemon) WritePID() error {
	if err := os.MkdirAll(d.cfg.DataDir, 0755); err != nil {
		return err
	}
	pid := os.Getpid()
	return os.WriteFile(d.PIDFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

func (d *Daemon) RemovePID() {
	_ = os.Remove(d.PIDFilePath())
}

func (d *Daemon) Start(ctx context.Context) error {
	if err := d.WritePID(); err != nil {
		return fmt.Errorf("failed to write PID: %w", err)
	}
	defer d.RemovePID()

	slog.Info("daemon started", "pid", os.Getpid())

	// Run initial sync on start
	_, _ = SyncAll(d.cfg, d.store, d.engine)

	watcher, err := fsnotify.NewWatcher()
	if err == nil {
		defer watcher.Close()
		for _, tool := range d.cfg.Tools {
			if !tool.Enabled {
				continue
			}
			for _, p := range tool.Paths {
				expanded := config.ExpandHome(p)
				_ = watcher.Add(expanded)
			}
		}
	}

	ticker := time.NewTicker(d.cfg.Daemon.PollInterval)
	defer ticker.Stop()

	for {
		select {
		case <-ctx.Done():
			slog.Info("daemon stopping gracefully")
			return nil
		case <-ticker.C:
			slog.Debug("running scheduled poll sync")
			_, _ = SyncAll(d.cfg, d.store, d.engine)
		case event, ok := <-watcher.Events:
			if !ok {
				continue
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				slog.Debug("fs event detected, syncing", "file", event.Name)
				_, _ = SyncAll(d.cfg, d.store, d.engine)
			}
		case err, ok := <-watcher.Errors:
			if ok {
				slog.Warn("fs watcher error", "err", err)
			}
		}
	}
}

func StopDaemon(pidPath string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("could not read PID file: %w", err)
	}
	pid, err := strconv.Atoi(string(data))
	if err != nil {
		return fmt.Errorf("invalid PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/daemon/... -v`
Expected: PASS

- [ ] **Step 5: Commit**

```bash
git add internal/daemon/
git commit -m "feat(daemon): implement synchronization engine and watcher daemon lifecycle"
```

---

### Task 8: Dashboard API & Web Frontend

**Files:**
- Create: `internal/store/queries.go`
- Create: `internal/dashboard/server.go`
- Create: `internal/dashboard/handlers.go`
- Create: `internal/dashboard/static/index.html`
- Create: `internal/dashboard/static/app.js`
- Create: `internal/dashboard/static/style.css`
- Test: `internal/dashboard/server_test.go`

**Interfaces:**
- Produces: Store queries: `GetOverview`, `GetCostOverTime`, `GetCostByModel`, `GetTokensByModel`, `GetToolUsage`, `GetRecentRequests`.
- Produces: `type Server struct`, `NewServer(cfg *config.Config, st *store.Store) *Server`, `(s *Server) Start() error`.

- [ ] **Step 1: Implement Store aggregation queries in `internal/store/queries.go`**

Create `internal/store/queries.go`:
```go
package store

import (
	"database/sql"
	"fmt"
	"time"
)

type TimeSeriesPoint struct {
	Date string  `json:"date"`
	Tool string  `json:"tool"`
	Cost float64 `json:"cost"`
}

type ModelCostPoint struct {
	Model        string  `json:"model"`
	Cost         float64 `json:"cost"`
	RequestCount int64   `json:"request_count"`
}

type ModelTokenPoint struct {
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

type ToolUsagePoint struct {
	Tool         string  `json:"tool"`
	RequestCount int64   `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
}

func periodClause(period string) string {
	switch period {
	case "today":
		return "WHERE timestamp >= date('now', 'start of day')"
	case "week":
		return "WHERE timestamp >= date('now', '-7 days')"
	case "month":
		return "WHERE timestamp >= date('now', '-30 days')"
	default:
		return ""
	}
}

func (s *Store) GetOverview(period string) (*OverviewStats, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT 
			count(*), 
			coalesce(sum(total_tokens), 0), 
			coalesce(sum(cost_usd), 0.0), 
			coalesce(avg(latency_ms), 0.0)
		FROM requests %s
	`, clause)

	var stats OverviewStats
	err := s.db.QueryRow(query).Scan(&stats.TotalRequests, &stats.TotalTokens, &stats.TotalCost, &stats.AvgLatencyMS)
	return &stats, err
}

func (s *Store) GetCostOverTime(period string) ([]TimeSeriesPoint, error) {
	clause := periodClause(period)
	where := clause
	if where == "" {
		where = "WHERE 1=1"
	}

	query := fmt.Sprintf(`
		SELECT strftime('%%Y-%%m-%%d', timestamp) as day, tool, coalesce(sum(cost_usd), 0.0)
		FROM requests
		%s
		GROUP BY day, tool
		ORDER BY day ASC
	`, where)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []TimeSeriesPoint
	for rows.Next() {
		var pt TimeSeriesPoint
		if err := rows.Scan(&pt.Date, &pt.Tool, &pt.Cost); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetCostByModel(period string) ([]ModelCostPoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT coalesce(model, 'unknown'), coalesce(sum(cost_usd), 0.0), count(*)
		FROM requests
		%s
		GROUP BY model
		ORDER BY sum(cost_usd) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ModelCostPoint
	for rows.Next() {
		var pt ModelCostPoint
		if err := rows.Scan(&pt.Model, &pt.Cost, &pt.RequestCount); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetTokensByModel(period string) ([]ModelTokenPoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT coalesce(model, 'unknown'), coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0)
		FROM requests
		%s
		GROUP BY model
		ORDER BY sum(total_tokens) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ModelTokenPoint
	for rows.Next() {
		var pt ModelTokenPoint
		if err := rows.Scan(&pt.Model, &pt.InputTokens, &pt.OutputTokens); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetToolUsage(period string) ([]ToolUsagePoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT tool, count(*), coalesce(sum(cost_usd), 0.0)
		FROM requests
		%s
		GROUP BY tool
		ORDER BY count(*) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var points []ToolUsagePoint
	for rows.Next() {
		var pt ToolUsagePoint
		if err := rows.Scan(&pt.Tool, &pt.RequestCount, &pt.TotalCost); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetRecentRequests(limit int) ([]Request, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, tool, session_id, timestamp, model, input_tokens, output_tokens, total_tokens, cost_usd, latency_ms, status
		FROM requests
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var reqs []Request
	for rows.Next() {
		var r Request
		var tsStr string
		var cost sql.NullFloat64
		var latency sql.NullInt64

		if err := rows.Scan(&r.ID, &r.Tool, &r.SessionID, &tsStr, &r.Model, &r.InputTokens, &r.OutputTokens, &r.TotalTokens, &cost, &latency, &r.Status); err == nil {
			r.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
			if cost.Valid {
				r.CostUSD = &cost.Float64
			}
			if latency.Valid {
				r.LatencyMS = &latency.Int64
			}
			reqs = append(reqs, r)
		}
	}
	return reqs, nil
}
```

- [ ] **Step 2: Write test for HTTP dashboard handlers and JSON API responses**

Create `internal/dashboard/server_test.go`:
```go
package dashboard_test

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/dashboard"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestDashboardAPI_Overview(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost := 0.50
	latency := int64(800)
	_, _ = st.InsertRequests([]store.Request{
		{
			ID: "r1", Tool: "claude_code", Timestamp: time.Now().UTC(),
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 200, TotalTokens: 1200,
			CostUSD: &cost, LatencyMS: &latency, Status: "success",
		},
	})

	cfg := config.DefaultConfig()
	srv := dashboard.NewServer(cfg, st)

	req := httptest.NewRequest("GET", "/api/overview?period=all", nil)
	w := httptest.NewRecorder()
	srv.Router().ServeHTTP(w, req)

	if w.Code != http.StatusOK {
		t.Fatalf("expected status 200, got %d", w.Code)
	}

	var stats store.OverviewStats
	if err := json.Unmarshal(w.Body.Bytes(), &stats); err != nil {
		t.Fatal(err)
	}
	if stats.TotalRequests != 1 {
		t.Fatalf("expected 1 request, got %d", stats.TotalRequests)
	}
	if stats.TotalTokens != 1200 {
		t.Fatalf("expected 1200 tokens, got %d", stats.TotalTokens)
	}
}
```

- [ ] **Step 3: Run test to verify it fails**

Run: `go test ./internal/dashboard/... -v`
Expected: FAIL

- [ ] **Step 4: Implement dashboard server, handlers, and embedded static assets**

Create `internal/dashboard/handlers.go`:
```go
package dashboard

import (
	"encoding/json"
	"net/http"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	stats, err := s.store.GetOverview(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleCostOverTime(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetCostOverTime(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleCostByModel(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetCostByModel(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleTokensByModel(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetTokensByModel(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleToolUsage(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetToolUsage(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleRecentRequests(w http.ResponseWriter, r *http.Request) {
	reqs, err := s.store.GetRecentRequests(50)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}
```

Create `internal/dashboard/server.go`:
```go
package dashboard

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	cfg    *config.Config
	store  *store.Store
	router *http.ServeMux
}

func NewServer(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:    cfg,
		store:  st,
		router: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Router() *http.ServeMux {
	return s.router
}

func (s *Server) routes() {
	s.router.HandleFunc("GET /api/overview", s.handleOverview)
	s.router.HandleFunc("GET /api/cost-over-time", s.handleCostOverTime)
	s.router.HandleFunc("GET /api/cost-by-model", s.handleCostByModel)
	s.router.HandleFunc("GET /api/tokens-by-model", s.handleTokensByModel)
	s.router.HandleFunc("GET /api/tool-usage", s.handleToolUsage)
	s.router.HandleFunc("GET /api/requests", s.handleRecentRequests)

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		s.router.Handle("/", http.FileServer(http.FS(sub)))
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Dashboard.Host, s.cfg.Dashboard.Port)
	return http.ListenAndServe(addr, s.router)
}
```

Create `internal/dashboard/static/index.html`:
```html
<!DOCTYPE html>
<html lang="en">
<head>
  <meta charset="UTF-8">
  <meta name="viewport" content="width=device-width, initial-scale=1.0">
  <title>LLM Cost Tracker</title>
  <link rel="stylesheet" href="style.css">
  <script src="https://cdn.jsdelivr.net/npm/chart.js"></script>
</head>
<body>
  <div class="container">
    <header>
      <h1>LLM Cost Tracker</h1>
      <div class="controls">
        <label for="period">Period:</label>
        <select id="period" onchange="loadDashboard()">
          <option value="today">Today</option>
          <option value="week" selected>Last 7 Days</option>
          <option value="month">Last 30 Days</option>
          <option value="all">All Time</option>
        </select>
      </div>
    </header>

    <div class="cards-grid">
      <div class="card">
        <h3>Total Requests</h3>
        <p id="total-requests" class="metric">-</p>
      </div>
      <div class="card">
        <h3>Total Tokens</h3>
        <p id="total-tokens" class="metric">-</p>
      </div>
      <div class="card">
        <h3>Total Cost</h3>
        <p id="total-cost" class="metric">-</p>
      </div>
      <div class="card">
        <h3>Avg Latency</h3>
        <p id="avg-latency" class="metric">-</p>
      </div>
    </div>

    <div class="charts-grid">
      <div class="chart-box">
        <h3>Cost Over Time</h3>
        <canvas id="costTimeChart"></canvas>
      </div>
      <div class="chart-box">
        <h3>Cost by Model</h3>
        <canvas id="costModelChart"></canvas>
      </div>
      <div class="chart-box">
        <h3>Tokens by Model</h3>
        <canvas id="tokensModelChart"></canvas>
      </div>
      <div class="chart-box">
        <h3>Tool Distribution</h3>
        <canvas id="toolUsageChart"></canvas>
      </div>
    </div>

    <div class="table-box">
      <h3>Recent Requests</h3>
      <table>
        <thead>
          <tr>
            <th>Time</th>
            <th>Tool</th>
            <th>Model</th>
            <th>Input Tok</th>
            <th>Output Tok</th>
            <th>Cost</th>
            <th>Latency</th>
          </tr>
        </thead>
        <tbody id="requests-table"></tbody>
      </table>
    </div>
  </div>
  <script src="app.js"></script>
</body>
</html>
```

Create `internal/dashboard/static/style.css`:
```css
:root {
  --bg: #0d1117;
  --surface: #161b22;
  --border: #30363d;
  --text: #c9d1d9;
  --accent: #58a6ff;
  --success: #3fb950;
}

body {
  margin: 0;
  font-family: -apple-system, BlinkMacSystemFont, "Segoe UI", Roboto, sans-serif;
  background-color: var(--bg);
  color: var(--text);
  padding: 24px;
}

.container {
  max-width: 1200px;
  margin: 0 auto;
}

header {
  display: flex;
  justify-content: space-between;
  align-items: center;
  border-bottom: 1px solid var(--border);
  padding-bottom: 16px;
  margin-bottom: 24px;
}

select {
  background: var(--surface);
  color: var(--text);
  border: 1px solid var(--border);
  padding: 6px 12px;
  border-radius: 6px;
}

.cards-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(220px, 1fr));
  gap: 16px;
  margin-bottom: 24px;
}

.card {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
}

.card h3 {
  margin: 0 0 8px 0;
  font-size: 0.9rem;
  color: #8b949e;
}

.metric {
  font-size: 1.8rem;
  font-weight: bold;
  margin: 0;
  color: var(--accent);
}

.charts-grid {
  display: grid;
  grid-template-columns: repeat(auto-fit, minmax(500px, 1fr));
  gap: 20px;
  margin-bottom: 24px;
}

.chart-box {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
}

.table-box {
  background: var(--surface);
  border: 1px solid var(--border);
  border-radius: 8px;
  padding: 16px;
  overflow-x: auto;
}

table {
  width: 100%;
  border-collapse: collapse;
}

th, td {
  text-align: left;
  padding: 10px;
  border-bottom: 1px solid var(--border);
  font-size: 0.9rem;
}

th {
  color: #8b949e;
}
```

Create `internal/dashboard/static/app.js`:
```javascript
let charts = {};

async function fetchJSON(url) {
  const res = await fetch(url);
  return res.json();
}

function formatTokens(num) {
  if (num >= 1000000) return (num / 1000000).toFixed(1) + 'M';
  if (num >= 1000) return (num / 1000).toFixed(1) + 'K';
  return num.toString();
}

async function loadDashboard() {
  const period = document.getElementById('period').value;

  const overview = await fetchJSON(`/api/overview?period=${period}`);
  document.getElementById('total-requests').innerText = overview.total_requests.toLocaleString();
  document.getElementById('total-tokens').innerText = formatTokens(overview.total_tokens);
  document.getElementById('total-cost').innerText = '$' + overview.total_cost.toFixed(2);
  document.getElementById('avg-latency').innerText = (overview.avg_latency_ms / 1000).toFixed(2) + 's';

  loadCostOverTime(period);
  loadCostByModel(period);
  loadTokensByModel(period);
  loadToolUsage(period);
  loadRequests();
}

async function loadCostOverTime(period) {
  const data = await fetchJSON(`/api/cost-over-time?period=${period}`);
  const labels = [...new Set(data.map(d => d.date))];
  const costs = labels.map(l => {
    return data.filter(d => d.date === l).reduce((acc, cur) => acc + cur.cost, 0);
  });

  if (charts.costTime) charts.costTime.destroy();
  charts.costTime = new Chart(document.getElementById('costTimeChart'), {
    type: 'line',
    data: {
      labels: labels,
      datasets: [{
        label: 'Cost ($)',
        data: costs,
        borderColor: '#58a6ff',
        tension: 0.2,
        fill: true,
        backgroundColor: 'rgba(88, 166, 255, 0.1)'
      }]
    },
    options: { responsive: true }
  });
}

async function loadCostByModel(period) {
  const data = await fetchJSON(`/api/cost-by-model?period=${period}`);
  if (charts.costModel) charts.costModel.destroy();
  charts.costModel = new Chart(document.getElementById('costModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [{
        label: 'Cost ($)',
        data: data.map(d => d.cost),
        backgroundColor: '#3fb950'
      }]
    },
    options: { indexAxis: 'y', responsive: true }
  });
}

async function loadTokensByModel(period) {
  const data = await fetchJSON(`/api/tokens-by-model?period=${period}`);
  if (charts.tokensModel) charts.tokensModel.destroy();
  charts.tokensModel = new Chart(document.getElementById('tokensModelChart'), {
    type: 'bar',
    data: {
      labels: data.map(d => d.model),
      datasets: [
        { label: 'Input Tokens', data: data.map(d => d.input_tokens), backgroundColor: '#1f6feb' },
        { label: 'Output Tokens', data: data.map(d => d.output_tokens), backgroundColor: '#a371f7' }
      ]
    },
    options: { responsive: true, scales: { x: { stacked: true }, y: { stacked: true } } }
  });
}

async function loadToolUsage(period) {
  const data = await fetchJSON(`/api/tool-usage?period=${period}`);
  if (charts.toolUsage) charts.toolUsage.destroy();
  charts.toolUsage = new Chart(document.getElementById('toolUsageChart'), {
    type: 'doughnut',
    data: {
      labels: data.map(d => d.tool),
      datasets: [{
        data: data.map(d => d.request_count),
        backgroundColor: ['#58a6ff', '#3fb950', '#d29922', '#db61a2', '#a371f7']
      }]
    },
    options: { responsive: true }
  });
}

async function loadRequests() {
  const reqs = await fetchJSON('/api/requests');
  const tbody = document.getElementById('requests-table');
  tbody.innerHTML = '';
  reqs.forEach(r => {
    const tr = document.createElement('tr');
    const cost = r.cost_usd ? '$' + r.cost_usd.toFixed(4) : '—';
    const latency = r.latency_ms ? (r.latency_ms / 1000).toFixed(2) + 's' : '—';
    tr.innerHTML = `
      <td>${new Date(r.timestamp).toLocaleTimeString()}</td>
      <td>${r.tool}</td>
      <td>${r.model || '—'}</td>
      <td>${r.input_tokens.toLocaleString()}</td>
      <td>${r.output_tokens.toLocaleString()}</td>
      <td>${cost}</td>
      <td>${latency}</td>
    `;
    tbody.appendChild(tr);
  });
}

document.addEventListener('DOMContentLoaded', () => {
  loadDashboard();
  setInterval(loadDashboard, 60000);
});
```

- [ ] **Step 5: Run tests and verify they pass**

Run: `go test ./internal/dashboard/... -v`
Expected: PASS

- [ ] **Step 6: Commit**

```bash
git add internal/store/queries.go internal/dashboard/
git commit -m "feat(dashboard): implement web dashboard UI, static server, and REST API handlers"
```

---

### Task 9: CLI Commands & Main Entrypoint

**Files:**
- Create: `internal/cli/root.go`
- Create: `internal/cli/daemon.go`
- Create: `internal/cli/sync.go`
- Create: `internal/cli/summary.go`
- Create: `internal/cli/dashboard.go`
- Create: `internal/cli/tools.go`
- Create: `cmd/llm-track/main.go`
- Test: `internal/cli/summary_test.go`

**Interfaces:**
- Produces: CLI binary `llm-track` with subcommands: `daemon`, `sync`, `summary`, `dashboard`, `tools`.

- [ ] **Step 1: Write test for summary formatting**

Create `internal/cli/summary_test.go`:
```go
package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/cli"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestRenderSummary(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost := 1.25
	lat := int64(1500)
	_, _ = st.InsertRequests([]store.Request{
		{
			ID: "1", Tool: "claude_code", Timestamp: time.Now().UTC(),
			Model: "claude-3-7-sonnet", InputTokens: 5000, OutputTokens: 1000, TotalTokens: 6000,
			CostUSD: &cost, LatencyMS: &lat, Status: "success",
		},
	})

	var buf bytes.Buffer
	err = cli.RenderSummary(&buf, st, "today")
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("LLM Cost Tracker")) {
		t.Fatalf("expected title in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("Total Cost:     $1.25")) {
		t.Fatalf("expected total cost $1.25 in output: %s", out)
	}
}
```

- [ ] **Step 2: Run test to verify it fails**

Run: `go test ./internal/cli/... -v`
Expected: FAIL

- [ ] **Step 3: Implement Cobra CLI commands and main entrypoint**

Create `internal/cli/root.go`:
```go
package cli

import (
	"fmt"
	"os"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

var (
	cfgPath string
	cfg     *config.Config
	st      *store.Store
	engine  *pricing.Engine
)

var rootCmd = &cobra.Command{
	Use:   "llm-track",
	Short: "Track cost, tokens, and latency for CLI-based LLM tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		return summaryCmd.RunE(cmd, args)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "~/.config/llm-cost-tracker/config.yaml", "config file path")

	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(summaryCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(toolsCmd)
}

func initConfig() {
	var err error
	cfg, err = config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	dbPath := fmt.Sprintf("%s/tracker.db", cfg.DataDir)
	st, err = store.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open store at %s: %v\n", dbPath, err)
		os.Exit(1)
	}

	engine = pricing.NewEngine(nil)
}
```

Create `internal/cli/summary.go`:
```go
package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

var periodFlag string

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show cost, token, and latency summary in the terminal",
	RunE: func(cmd *cobra.Command, args []string) error {
		return RenderSummary(os.Stdout, st, periodFlag)
	},
}

func init() {
	summaryCmd.Flags().StringVar(&periodFlag, "period", "today", "period to display: today, week, month, all")
}

func RenderSummary(w io.Writer, s *store.Store, period string) error {
	stats, err := s.GetOverview(period)
	if err != nil {
		return err
	}

	toolUsage, _ := s.GetToolUsage(period)
	modelCost, _ := s.GetCostByModel(period)

	fmt.Fprintln(w, "╭─────────────────────────────────────────────╮")
	fmt.Fprintf(w, "│           LLM Cost Tracker — %-14s │\n", period)
	fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│  Total Requests: %-26d │\n", stats.TotalRequests)
	fmt.Fprintf(w, "│  Total Tokens:   %-26d │\n", stats.TotalTokens)
	fmt.Fprintf(w, "│  Total Cost:     $%-25.2f │\n", stats.TotalCost)
	fmt.Fprintf(w, "│  Avg Latency:    %-24.2fs │\n", stats.AvgLatencyMS/1000.0)

	if len(toolUsage) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Tool:                                   │")
		for _, tu := range toolUsage {
			fmt.Fprintf(w, "│    %-12s %3d reqs   $%-6.2f          │\n", tu.Tool, tu.RequestCount, tu.TotalCost)
		}
	}

	if len(modelCost) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Model:                                  │")
		for _, mc := range modelCost {
			fmt.Fprintf(w, "│    %-16s %3d reqs  $%-6.2f        │\n", mc.Model, mc.RequestCount, mc.Cost)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────╯")
	return nil
}
```

Create `internal/cli/sync.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
)

var syncCmd = &cobra.Command{
	Use:   "sync",
	Short: "Scan all tool directories and import new records once",
	RunE: func(cmd *cobra.Command, args []string) error {
		fmt.Println("Scanning tool directories for new usage data...")
		imported, err := daemon.SyncAll(cfg, st, engine)
		if err != nil {
			return err
		}
		fmt.Printf("Sync complete. Imported %d new record(s).\n", imported)
		return nil
	},
}
```

Create `internal/cli/daemon.go`:
```go
package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"syscall"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the background watcher daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background watcher daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		d := daemon.NewDaemon(cfg, st, engine)

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()

		fmt.Printf("Starting LLM Cost Tracker daemon (PID %d)...\n", os.Getpid())
		return d.Start(ctx)
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running background daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile := filepath.Join(cfg.DataDir, "daemon.pid")
		if err := daemon.StopDaemon(pidFile); err != nil {
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		fmt.Println("Daemon stopped successfully.")
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
}
```

Create `internal/cli/dashboard.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/dashboard"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard server",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := dashboard.NewServer(cfg, st)
		fmt.Printf("Dashboard server running at http://%s:%d\n", cfg.Dashboard.Host, cfg.Dashboard.Port)
		return srv.Start()
	},
}
```

Create `internal/cli/tools.go`:
```go
package cli

import (
	"fmt"

	"github.com/spf13/cobra"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
)

var toolsCmd = &cobra.Command{
	Use:   "tools",
	Short: "List supported and discovered CLI tools",
	Run: func(cmd *cobra.Command, args []string) {
		fmt.Println("Configured Tools:")
		for name, toolCfg := range cfg.Tools {
			status := "disabled"
			if toolCfg.Enabled {
				status = "enabled"
			}
			p, registered := parsers.Get(name)
			sourceCount := 0
			if registered {
				sources, _ := p.Discover(toolCfg)
				sourceCount = len(sources)
			}
			fmt.Printf("  • %-12s [%s] found %d active log source(s)\n", name, status, sourceCount)
		}
	},
}
```

Create `cmd/llm-track/main.go`:
```go
package main

import "github.com/nehaverma/llm-cost-tracker/internal/cli"

func main() {
	cli.Execute()
}
```

- [ ] **Step 4: Run tests and verify they pass**

Run: `go test ./internal/cli/... -v`
Expected: PASS

- [ ] **Step 5: Build and test binary execution**

Run:
```bash
go build -o bin/llm-track ./cmd/llm-track
./bin/llm-track --help
./bin/llm-track tools
```

- [ ] **Step 6: Commit**

```bash
git add internal/cli/ cmd/
git commit -m "feat(cli): implement Cobra CLI root, daemon, sync, summary, and dashboard commands"
```

---

### Task 10: End-to-End Verification & Documentation

**Files:**
- Create: `README.md`
- Test: `test/e2e_test.go`

- [ ] **Step 1: Write comprehensive end-to-end integration test**

Create `test/e2e_test.go`:
```go
package test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestEndToEndPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "e2e.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	fixtureDir, _ := filepath.Abs("../internal/parsers/testdata")
	cfg := config.DefaultConfig()
	cfg.Tools["claude_code"] = config.ToolConfig{Enabled: true, Paths: []string{fixtureDir}}
	cfg.Tools["agy"] = config.ToolConfig{Enabled: true, Paths: []string{fixtureDir}}
	cfg.Tools["aider"] = config.ToolConfig{Enabled: false}
	cfg.Tools["codex"] = config.ToolConfig{Enabled: false}
	cfg.Tools["opencode"] = config.ToolConfig{Enabled: false}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("sync error: %v", err)
	}
	if imported < 2 {
		t.Fatalf("expected at least 2 records imported, got %d", imported)
	}

	overview, err := st.GetOverview("all")
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalRequests == 0 || overview.TotalTokens == 0 {
		t.Fatalf("expected positive metrics in overview: %+v", overview)
	}
}
```

- [ ] **Step 2: Run end-to-end integration test**

Run: `go test ./test/... -v`
Expected: PASS

- [ ] **Step 3: Create README.md documenting setup, CLI usage, and dashboard**

Create `README.md`:
```markdown
# LLM Cost Tracker (`llm-track`)

A fast, lightweight Go daemon and CLI that automatically tracks costs, tokens, and latencies across CLI coding assistants (Claude Code, AGY, Codex, Aider, OpenCode) with zero workflow changes.

## Features

- **Non-Invasive**: Reads local log files and databases (`~/.claude`, `~/.gemini/antigravity-cli`, `~/.codex`, etc.).
- **Automatic Daemon**: Background file watcher using `fsnotify` + fallback polling.
- **Deduplicated Local Store**: Embedded SQLite database with automatic incremental offsets.
- **Terminal Summary**: Instant cost, token, and model overview in your terminal.
- **Interactive Web Dashboard**: Chart.js visual dashboard showing cost trends, model breakdown, and token usage.

## Installation

```bash
go build -o /usr/local/bin/llm-track ./cmd/llm-track
```

## Quick Start

```bash
# Scan all local tool directories and import existing history
llm-track sync

# View today's summary in terminal
llm-track summary

# Start the background watcher daemon
llm-track daemon start

# Launch the browser dashboard (http://127.0.0.1:8384)
llm-track dashboard
```
```

- [ ] **Step 4: Run all project tests**

Run: `go test ./... -v`
Expected: ALL PASS

- [ ] **Step 5: Commit**

```bash
git add test/ README.md
git commit -m "docs: add README and end-to-end integration verification suite"
```
