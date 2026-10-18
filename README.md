# LLM Cost Tracker (`llm-track`)

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/MasterZeR0/llm-cost-tracker)](go.mod)

A fast, lightweight Go daemon and CLI that automatically tracks costs, tokens, latencies, and cache efficiency across CLI coding assistants (**Claude Code**, **AGY / Gemini**, **Codex**, **Aider**, **OpenCode**) with zero workflow changes.

---

## Features

- **Non-Invasive**: Automatically discovers and reads existing local logs and databases (`~/.claude`, `~/.gemini/antigravity-cli`, `~/.codex`, etc.).
- **Background Daemon**: File watcher powered by `fsnotify` with smart incremental offset tracking and fallback polling.
- **Embedded SQLite Store**: Self-healing local database with automatic migrations and deduplication.
- **Prompt Caching Analytics**: Ingests cache hits ($T_{\text{hit}}$), misses ($T_{\text{miss}}$), cache hit rate (%), and computes exact USD savings.
- **Session & Turn Progression**: Reconstructs multi-turn sessions with durations, turn counts, and step chronology.
- **Agentic Tool Observability**: Detects tool executions (`run_command`, `view_file`, `write_to_file`, subagents) and tracks error rates.
- **Terminal Summary & Drilldown**:
  - `llm-track summary`: Instant high-level cost, token, latency, and cache overview.
  - `llm-track sessions`: Interactive session list and turn-by-turn drilldown table.
- **Interactive Web Dashboard**: Embedded Chart.js dashboard displaying cost over time, model breakdown, tool usage, token composition, and session inspector.

---

## Installation

### Via `go install` (Recommended)

```bash
go install github.com/MasterZeR0/llm-cost-tracker/cmd/llm-track@latest
```

### From Source

```bash
git clone https://github.com/MasterZeR0/llm-cost-tracker.git
cd llm-cost-tracker
go build -o /usr/local/bin/llm-track ./cmd/llm-track
```

---

## Quick Start

```bash
# Scan local tool directories and import existing history
llm-track sync

# View today's summary in terminal (also accepts --period=week|month|all)
llm-track summary

# List recent multi-turn sessions with cache % and durations
llm-track sessions --period=today

# Deep-dive into a specific session's turn chronology and tool calls
llm-track sessions <session-id-or-prefix>

# Start the background watcher daemon
llm-track daemon start

# Launch the interactive web dashboard (http://127.0.0.1:8384)
llm-track dashboard
```

---

## Architecture

- **`internal/parsers/`**: Tool-specific ingestors (JSONL, SQLite WAL, protobuf step decoders) with incremental state tracking.
- **`internal/pricing/`**: Embedded model pricing catalog with prefix-matching and per-million token rates (miss, hit, write, output).
- **`internal/store/`**: SQLite persistence layer with thread-safe queries, session aggregations, and migration engine.
- **`internal/daemon/`**: Filesystem notification loop and automatic backfiller.
- **`internal/dashboard/`**: Zero-dependency embedded web server (`embed.FS`) and RESTful analytics API.

---

## License

This project is licensed under the [MIT License](LICENSE).
