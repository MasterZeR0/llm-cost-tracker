# LLM Cost Tracker (`llm-track`)

[![License: MIT](https://img.shields.io/badge/License-MIT-blue.svg)](LICENSE)
[![Go Version](https://img.shields.io/github/go-mod/go-version/MasterZeR0/llm-cost-tracker)](go.mod)

A fast, lightweight Go daemon and CLI that automatically tracks costs, tokens, latencies, and cache efficiency across AI coding assistants and IDEs (**Claude Code**, **AGY / Gemini**, **Codex**, **Aider**, **OpenCode**, **Cline**, **Roo Code**, **Cursor**, **Windsurf**, **GitHub Copilot CLI**) with zero workflow changes.

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

## Supported Tools & LLMs

### Supported CLI Assistants

| Assistant | Data Source | Metrics Tracked |
| :--- | :--- | :--- |
| **Claude Code** | `~/.claude/projects/` | Input/Output tokens, prompt cache read/write, turn cost, tool calls, errors, latency |
| **AGY (Antigravity CLI)** | `~/.gemini/antigravity-cli/` | Multi-turn steps, subagent dispatches, tool executions, exact generation latency, token caching |
| **Codex CLI** | `~/.codex/` | Thread sessions, model providers, token usage, duration |
| **Aider** | `.aider.analytics.jsonl` | Message turns, completion tokens, prompt tokens, model costs |
| **OpenCode** | `~/.local/share/opencode/` | Message history, session IDs, model allocations, token splits |
| **Cline** | `saoudrizwan.claude-dev/tasks/` | Multi-turn task steps, prompt cache read/write, cost, tools (`execute_command`, `write_to_file`) |
| **Roo Code** | `rooveterinaryinc.roo-cline/tasks/` | Independent task sessions, custom mode tracking, prompt caching, tool error detection |
| **Cursor** | `Cursor/.../state.vscdb` | Composer & chat history, turn bubbles, hybrid token estimation, tool actions |
| **Windsurf** | `Windsurf/.../state.vscdb`, `cascade/` | Cascade sessions, multi-step turns, tool calls, latencies, cost calculations |
| **GitHub Copilot CLI** | `~/.copilot-cli/`, `~/.config/gh/` | Command suggestions, explanation turns, hybrid token estimation, model mapping |

### Supported Models & Pricing Catalog

`llm-track` includes a built-in pricing engine with prefix-matching and per-million token rates:

* **Anthropic Family**:
  * Claude Opus 4.x (4.5, 4.6, 4.7, 4.8) & Claude Opus 5
  * Claude 3 Opus & Claude 3.7 / 3.5 Sonnet
  * Claude 3.5 Haiku & Claude 3 Haiku
* **Google Gemini Family**:
  * Gemini 3 Flash & Gemini 3 Pro
  * Gemini 2.5 Pro
  * Gemini 2.0 Flash & Gemini 2.0 Pro
  * Gemini 1.5 Flash & Gemini 1.5 Pro
* **OpenAI Family**:
  * GPT-4.5 & GPT-4.5-preview
  * GPT-4o & GPT-4o-mini
  * GPT-4 Turbo & GPT-4
  * o1 & o1-mini
  * o3, o3-mini & o3-pro
* **DeepSeek Family**:
  * DeepSeek-V3 (`deepseek-chat`, `deepseek-v3`, `deepseek-coder`)
  * DeepSeek-R1 (`deepseek-reasoner`, `deepseek-r1`)

*(Custom pricing rates and unpriced/internal assistant models like `cursor-small` or `cascade-base` can also be configured dynamically via the embedded pricing catalog or custom config).*

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

## Future Scope & Roadmap

- [x] **IDE & Extension Assistant Adapters**:
  - Direct log & session ingestion for **Cline**, **Roo Code**, **Cursor**, **Windsurf**, and **GitHub Copilot CLI**.
- [ ] **Local LLM Runtimes (Phase 2)**:
  - Local LLM tracking for **Ollama** and **vLLM** endpoints.
- [ ] **Tiered & Context-Window-Dependent Pricing**:
  - Threshold pricing support for long-context models (e.g., Gemini Pro $\le 128\text{k}$ vs $> 128\text{k}$ token pricing tiers).
  - Dynamic rate scaling based on total prompt + cache context token counts per turn.
- [ ] **Budgeting & Spend Threshold Alerts**:
  - Daily/weekly/monthly spend budgets with configurable limits.
  - Webhook, Slack, Discord, and desktop notifications when approaching or exceeding cost ceilings.
- [ ] **Team & Multi-Device Sync**:
  - Centralized aggregation server option (remote sync daemon) for developer teams and engineering orgs.
  - Export backend adapters for **PostgreSQL**, **ClickHouse**, or **TimescaleDB**.
- [ ] **Token Context Bloat & Waste Optimization**:
  - Context window growth visualization to detect repetitive file re-reading or prompt inflation.
  - Automated recommendations for context truncation or compaction.
- [ ] **Observability & Exporters**:
  - Native Prometheus `/metrics` exporter endpoint.
  - Pre-built Grafana dashboard templates and OpenTelemetry tracing integrations.

---

## License

This project is licensed under the [MIT License](LICENSE).
