package vscdb

import (
	"database/sql"
	"path/filepath"
	"testing"

	_ "github.com/mattn/go-sqlite3"
)

func createMockVSCDB(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	dbPath := filepath.Join(dir, "state.vscdb")

	db, err := sql.Open("sqlite3", dbPath)
	if err != nil {
		t.Fatalf("failed to create db: %v", err)
	}
	defer db.Close()

	_, err = db.Exec(`
		CREATE TABLE ItemTable (key TEXT PRIMARY KEY, value TEXT);
		INSERT INTO ItemTable (key, value) VALUES ('composer.composerData', '{"composers": []}');
		INSERT INTO ItemTable (key, value) VALUES ('workbench.panel.aichat.chatdata', '{"chats": []}');
	`)
	if err != nil {
		t.Fatalf("failed to populate mock vscdb: %v", err)
	}
	return dbPath
}

func TestVSCDBReader(t *testing.T) {
	dbPath := createMockVSCDB(t)

	reader, err := Open(dbPath)
	if err != nil {
		t.Fatalf("Open failed: %v", err)
	}
	defer reader.Close()

	val, err := reader.GetKey("composer.composerData")
	if err != nil {
		t.Fatalf("GetKey failed: %v", err)
	}
	if string(val) != `{"composers": []}` {
		t.Errorf("unexpected value: %s", string(val))
	}

	keys, err := reader.GetKeysLike("workbench.%")
	if err != nil {
		t.Fatalf("GetKeysLike failed: %v", err)
	}
	if len(keys) != 1 {
		t.Errorf("expected 1 key, got %d", len(keys))
	}

	// Missing key returns nil, nil
	missing, err := reader.GetKey("non_existent_key")
	if err != nil {
		t.Fatalf("expected nil error for missing key, got %v", err)
	}
	if missing != nil {
		t.Errorf("expected nil value for missing key, got %v", missing)
	}
}
