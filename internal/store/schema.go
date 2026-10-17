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
