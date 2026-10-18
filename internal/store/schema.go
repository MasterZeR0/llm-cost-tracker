package store

const SchemaSQL = `
PRAGMA journal_mode=WAL;
PRAGMA busy_timeout=5000;

CREATE TABLE IF NOT EXISTS requests (
    id                  TEXT PRIMARY KEY,
    tool                TEXT NOT NULL,
    session_id          TEXT,
    timestamp           DATETIME NOT NULL,
    model               TEXT,
    input_tokens        INTEGER DEFAULT 0,
    output_tokens       INTEGER DEFAULT 0,
    cache_read_tokens   INTEGER DEFAULT 0,
    cache_write_tokens  INTEGER DEFAULT 0,
    total_tokens        INTEGER DEFAULT 0,
    cost_usd            REAL,
    latency_ms          INTEGER,
    status              TEXT DEFAULT 'success',
    prompt_text         TEXT,
    response_text       TEXT,
    source_file         TEXT,
    raw_data            TEXT,
    imported_at         DATETIME DEFAULT CURRENT_TIMESTAMP,
    step_index          INTEGER DEFAULT 0,
    tool_call_count     INTEGER DEFAULT 0,
    tool_error_count    INTEGER DEFAULT 0,
    subagent_call_count INTEGER DEFAULT 0,
    UNIQUE(tool, session_id, timestamp, model)
);

CREATE TABLE IF NOT EXISTS tool_calls (
    id          TEXT PRIMARY KEY,
    request_id  TEXT NOT NULL,
    session_id  TEXT NOT NULL,
    tool_name   TEXT NOT NULL,
    status      TEXT DEFAULT 'success',
    created_at  DATETIME NOT NULL,
    FOREIGN KEY(request_id) REFERENCES requests(id) ON DELETE CASCADE
);

CREATE INDEX IF NOT EXISTS idx_tool_calls_session ON tool_calls(session_id);
CREATE INDEX IF NOT EXISTS idx_tool_calls_name ON tool_calls(tool_name);
CREATE INDEX IF NOT EXISTS idx_tool_calls_status ON tool_calls(status);

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
CREATE INDEX IF NOT EXISTS idx_requests_session ON requests(session_id);
CREATE INDEX IF NOT EXISTS idx_requests_tool ON requests(tool);
CREATE INDEX IF NOT EXISTS idx_requests_model ON requests(model);
CREATE INDEX IF NOT EXISTS idx_requests_tool_timestamp ON requests(tool, timestamp);
CREATE INDEX IF NOT EXISTS idx_tool_calls_request ON tool_calls(request_id);
`

