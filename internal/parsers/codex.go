package parsers

import (
	"database/sql"
	"os"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
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

	// Defensive check: does threads table exist?
	var tblExists int
	_ = db.QueryRow("SELECT count(*) FROM sqlite_master WHERE type='table' AND name='threads'").Scan(&tblExists)
	if tblExists == 0 {
		return nil, state, nil
	}

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
			ID:           uuid.New().String(),
			Tool:         "codex",
			SessionID:    id,
			Timestamp:    ts,
			Model:        modelStr,
			TotalTokens:  tokVal,
			InputTokens:  tokVal / 2, // Estimated split when not reported
			OutputTokens: tokVal / 2,
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
