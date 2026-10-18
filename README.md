# LLM Cost Tracker (`llm-track`)

A fast, lightweight Go daemon and CLI that automatically tracks costs, tokens, and latencies across CLI coding assistants (Claude Code, AGY, Codex, Aider, OpenCode) with zero workflow changes.

## Features

- **Non-Invasive**: Reads local log files and databases (`~/.claude`, `~/.gemini/antigravity-cli`, `~/.codex`, etc.).
- **Automatic Daemon**: Background file watcher using `fsnotify` + fallback polling.
- **Deduplicated Local Store**: Embedded SQLite database with automatic incremental offsets and migrations.
- **Prompt Cache Analytics**: Tracks cache hits ($T_{\text{hit}}$), misses ($T_{\text{miss}}$), cache hit rate (%), and calculated USD savings.
- **Session Lifecycles & Turns**: Groups interactions into multi-turn sessions with duration, turn counts, and token curves.
- **Agentic Tool Tracking**: Discovers and logs tool invocations (`run_command`, `view_file`, `write_to_file`, subagent launches) and tracks failure rates.
- **Terminal Summary & Drilldown**:
  - `llm-track summary`: Instant high-level cost, session, token, and cache overview.
  - `llm-track sessions`: Interactive session list and turn-by-turn drilldown table.
- **Interactive Web Dashboard**: Chart.js visual dashboard showing cost trends, tool breakdown, token composition, and session explorer.

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

# List recent multi-turn sessions with cache % and durations
llm-track sessions --period=today

# Deep-dive into a specific session's turn chronology and tool calls
llm-track sessions <session-id-or-prefix>

# Start the background watcher daemon
llm-track daemon start

# Launch the browser dashboard (http://127.0.0.1:8384)
llm-track dashboard
```

