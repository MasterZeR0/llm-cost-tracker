package parsers

import (
	"database/sql"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/google/uuid"
	_ "github.com/mattn/go-sqlite3"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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

	var isIntegerCol bool
	var hasMillis bool
	var sampleVal any
	_ = db.QueryRow("SELECT created_at FROM threads WHERE created_at IS NOT NULL LIMIT 1").Scan(&sampleVal)
	if sampleVal != nil {
		switch v := sampleVal.(type) {
		case int64:
			isIntegerCol = true
			if v > 100_000_000_000 {
				hasMillis = true
			}
		case int, int32, float64:
			isIntegerCol = true
		}
	} else {
		var pragmaRows *sql.Rows
		pragmaRows, err = db.Query("PRAGMA table_info(threads)")
		if err == nil {
			for pragmaRows.Next() {
				var cid, notNull, pk int
				var name, colType string
				var dflt sql.NullString
				if err := pragmaRows.Scan(&cid, &name, &colType, &notNull, &dflt, &pk); err == nil {
					if name == "created_at" && strings.Contains(strings.ToUpper(colType), "INT") {
						isIntegerCol = true
					}
				}
			}
			pragmaRows.Close()
		}
	}

	query := `
		SELECT id, model_provider, tokens_used, created_at
		FROM threads
	`
	var rows *sql.Rows
	if state.LastModified != nil {
		query += " WHERE created_at > ? ORDER BY created_at ASC"
		if isIntegerCol {
			if hasMillis {
				rows, err = db.Query(query, state.LastModified.UnixMilli())
			} else {
				rows, err = db.Query(query, state.LastModified.Unix())
			}
		} else {
			rows, err = db.Query(query, state.LastModified)
		}
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
	turnIndex := int64(0)

	for rows.Next() {
		turnIndex++
		var id string
		var model sql.NullString
		var tokens sql.NullInt64
		var rawCreatedAt any

		if err := rows.Scan(&id, &model, &tokens, &rawCreatedAt); err != nil {
			continue
		}

		ts := time.Now().UTC()
		if parsedTime, ok := parseCodexTimestamp(rawCreatedAt); ok {
			ts = parsedTime
			lastTime = &ts
		}
		// Sub-second offset to avoid same-second collision on deduplication
		ts = ts.Add(time.Duration(turnIndex) * time.Millisecond)
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

func parseCodexTimestamp(val any) (time.Time, bool) {
	if val == nil {
		return time.Time{}, false
	}
	switch v := val.(type) {
	case int64:
		return parseEpoch(v)
	case int:
		return parseEpoch(int64(v))
	case int32:
		return parseEpoch(int64(v))
	case float64:
		return parseEpoch(int64(v))
	case time.Time:
		return v.UTC(), true
	case string:
		return parseCodexStringTime(v)
	case []byte:
		return parseCodexStringTime(string(v))
	default:
		return time.Time{}, false
	}
}

func parseEpoch(epoch int64) (time.Time, bool) {
	if epoch <= 0 {
		return time.Time{}, false
	}
	// > 1e11 (100 billion) corresponds to milliseconds (> year 1973 in millis, but year 5138 in seconds)
	if epoch > 100_000_000_000 {
		return time.UnixMilli(epoch).UTC(), true
	}
	return time.Unix(epoch, 0).UTC(), true
}

func parseCodexStringTime(s string) (time.Time, bool) {
	s = strings.TrimSpace(s)
	if s == "" {
		return time.Time{}, false
	}
	if num, err := strconv.ParseInt(s, 10, 64); err == nil {
		return parseEpoch(num)
	}
	layouts := []string{
		time.RFC3339Nano,
		time.RFC3339,
		"2006-01-02 15:04:05.999999999-07:00",
		"2006-01-02 15:04:05.999999999",
		"2006-01-02 15:04:05",
		"2006-01-02T15:04:05",
		"2006-01-02",
	}
	for _, layout := range layouts {
		if t, err := time.Parse(layout, s); err == nil {
			return t.UTC(), true
		}
	}
	return time.Time{}, false
}
