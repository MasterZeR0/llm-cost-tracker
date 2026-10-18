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

	st := &Store{db: db}
	if err := st.migrate(); err != nil {
		db.Close()
		return nil, fmt.Errorf("failed to run migrations: %w", err)
	}

	return st, nil
}

func (s *Store) migrate() error {
	cols := map[string]string{
		"step_index":          "INTEGER DEFAULT 0",
		"tool_call_count":     "INTEGER DEFAULT 0",
		"tool_error_count":    "INTEGER DEFAULT 0",
		"subagent_call_count": "INTEGER DEFAULT 0",
	}

	for col, colType := range cols {
		var count int
		_ = s.db.QueryRow(`SELECT count(*) FROM pragma_table_info('requests') WHERE name = ?`, col).Scan(&count)
		if count == 0 {
			if _, err := s.db.Exec(`ALTER TABLE requests ADD COLUMN ` + col + ` ` + colType); err != nil {
				return err
			}
		}
	}
	return nil
}

func (s *Store) Close() error {
	return s.db.Close()
}

func (s *Store) DB() *sql.DB {
	return s.db
}

func (s *Store) InsertRequests(reqs []Request) (int, error) {
	return s.insertRequestsInternal(reqs, true)
}


func (s *Store) InsertRequestsWithTools(reqs []Request) error {
	_, err := s.insertRequestsInternal(reqs, true)
	return err
}

func (s *Store) insertRequestsInternal(reqs []Request, saveTools bool) (int, error) {
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
			source_file, raw_data, step_index, tool_call_count, tool_error_count, subagent_call_count
		) VALUES (?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?, ?)
	`)
	if err != nil {
		return 0, err
	}
	defer stmt.Close()

	var toolStmt *sql.Stmt
	if saveTools {
		toolStmt, err = tx.Prepare(`
			INSERT OR IGNORE INTO tool_calls (
				id, request_id, session_id, tool_name, status, created_at
			) VALUES (?, ?, ?, ?, ?, ?)
		`)
		if err != nil {
			return 0, err
		}
		defer toolStmt.Close()
	}

	var inserted int
	for _, r := range reqs {
		res, err := stmt.Exec(
			r.ID, r.Tool, r.SessionID, r.Timestamp.Format(time.RFC3339Nano), r.Model,
			r.InputTokens, r.OutputTokens, r.CacheReadTokens, r.CacheWriteTokens, r.TotalTokens,
			r.CostUSD, r.LatencyMS, r.Status, r.PromptText, r.ResponseText,
			r.SourceFile, r.RawData, r.StepIndex, r.ToolCallCount, r.ToolErrorCount, r.SubagentCallCount,
		)
		if err != nil {
			return 0, err
		}
		rows, _ := res.RowsAffected()
		if rows > 0 {
			inserted++
		}

		if saveTools && len(r.ToolCalls) > 0 {
			for _, tc := range r.ToolCalls {
				tcTime := tc.CreatedAt
				if tcTime.IsZero() {
					tcTime = r.Timestamp
				}
				tcReqID := tc.RequestID
				if tcReqID == "" {
					tcReqID = r.ID
				}
				tcSessID := tc.SessionID
				if tcSessID == "" {
					tcSessID = r.SessionID
				}
				tcStatus := tc.Status
				if tcStatus == "" {
					tcStatus = "success"
				}
				_, err := toolStmt.Exec(
					tc.ID, tcReqID, tcSessID, tc.ToolName, tcStatus, tcTime.Format(time.RFC3339Nano),
				)
				if err != nil {
					return 0, err
				}
			}
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
