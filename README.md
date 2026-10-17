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
