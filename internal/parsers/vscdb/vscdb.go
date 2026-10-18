package vscdb

import (
	"database/sql"
	"fmt"
	"net/url"

	_ "github.com/mattn/go-sqlite3"
)

type Reader struct {
	db *sql.DB
}

func Open(path string) (*Reader, error) {
	// Open in immutable read-only mode so editor processes are not locked.
	dsn := fmt.Sprintf("file:%s?mode=ro&immutable=1", url.PathEscape(path))
	db, err := sql.Open("sqlite3", dsn)
	if err != nil {
		return nil, fmt.Errorf("open vscdb: %w", err)
	}

	// Verify ItemTable exists
	var name string
	err = db.QueryRow("SELECT name FROM sqlite_master WHERE type='table' AND name='ItemTable'").Scan(&name)
	if err != nil {
		db.Close()
		return nil, fmt.Errorf("ItemTable not found in %s: %w", path, err)
	}

	return &Reader{db: db}, nil
}

func (r *Reader) GetKey(key string) ([]byte, error) {
	var val []byte
	err := r.db.QueryRow("SELECT value FROM ItemTable WHERE key = ?", key).Scan(&val)
	if err == sql.ErrNoRows {
		return nil, nil
	}
	if err != nil {
		return nil, err
	}
	return val, nil
}

func (r *Reader) GetKeysLike(pattern string) (map[string][]byte, error) {
	rows, err := r.db.Query("SELECT key, value FROM ItemTable WHERE key LIKE ?", pattern)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	results := make(map[string][]byte)
	for rows.Next() {
		var k string
		var v []byte
		if err := rows.Scan(&k, &v); err != nil {
			return nil, err
		}
		results[k] = v
	}
	return results, nil
}

func (r *Reader) Close() error {
	if r.db != nil {
		return r.db.Close()
	}
	return nil
}
