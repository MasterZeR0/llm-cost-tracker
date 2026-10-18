package parsers

import (
	"database/sql"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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

	var rows *sql.Rows
	if state.LastModified != nil {
		rows, err = db.Query(`
			SELECT id, session_id, model, tokens_in, tokens_out, created_at
			FROM messages
			WHERE created_at > ?
			ORDER BY created_at ASC
		`, state.LastModified)
	} else {
		rows, err = db.Query(`
			SELECT id, session_id, model, tokens_in, tokens_out, created_at
			FROM messages
			ORDER BY created_at ASC
		`)
	}
	if err != nil {
		return nil, state, nil // Table structure may differ across versions; fail safe
	}
	defer rows.Close()

	var requests []store.Request
	var lastID string
	var lastTime *time.Time

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
			lastTime = &ts
		}
		lastID = id

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

	if lastID != "" {
		state.LastRowID = lastID
	}
	if lastTime != nil {
		state.LastModified = lastTime
	}

	return requests, state, nil
}
