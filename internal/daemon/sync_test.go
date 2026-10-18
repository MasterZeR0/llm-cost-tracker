package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestSyncAll_EndToEnd(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	// Set config pointing claude_code to testdata fixture
	fixtureDir, _ := filepath.Abs("../parsers/testdata")
	cfg := config.DefaultConfig()
	cfg.Tools["claude_code"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{fixtureDir},
	}
	cfg.Tools["agy"] = config.ToolConfig{Enabled: false}
	cfg.Tools["aider"] = config.ToolConfig{Enabled: false}
	cfg.Tools["codex"] = config.ToolConfig{Enabled: false}
	cfg.Tools["opencode"] = config.ToolConfig{Enabled: false}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if imported != 2 {
		t.Fatalf("expected 2 imported records from sample, got %d", imported)
	}

	// Re-sync should import 0 because offset advanced
	importedAgain, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("second sync failed: %v", err)
	}
	if importedAgain != 0 {
		t.Fatalf("expected 0 new records on second sync, got %d", importedAgain)
	}
}

func TestSyncAll_PricingCalculation(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	// Create simulated AGY transcript file
	logDir := filepath.Join(tmpDir, "brain", "conv-test", ".system_generated", "logs")
	if err := os.MkdirAll(logDir, 0755); err != nil {
		t.Fatal(err)
	}
	transcriptPath := filepath.Join(logDir, "transcript.jsonl")
	content := `{"step_index":0,"source":"MODEL","type":"PLANNER_RESPONSE","status":"DONE","created_at":"2026-10-17T12:00:00Z","model":"gemini-2.0-flash","input_tokens":1000000,"output_tokens":1000000,"cache_read_tokens":0}` + "\n"
	if err := os.WriteFile(transcriptPath, []byte(content), 0644); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.Tools["agy"] = config.ToolConfig{
		Enabled: true,
		Paths:   []string{tmpDir},
	}
	cfg.Tools["claude_code"] = config.ToolConfig{Enabled: false}
	cfg.Tools["aider"] = config.ToolConfig{Enabled: false}
	cfg.Tools["codex"] = config.ToolConfig{Enabled: false}
	cfg.Tools["opencode"] = config.ToolConfig{Enabled: false}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("sync failed: %v", err)
	}
	if imported != 1 {
		t.Fatalf("expected 1 imported record, got %d", imported)
	}

	// Verify cost was calculated by pricing engine (gemini-2-flash: 0.10 in / 0.40 out -> 0.50)
	var cost float64
	err = st.DB().QueryRow("SELECT cost_usd FROM requests WHERE tool = 'agy' LIMIT 1").Scan(&cost)
	if err != nil {
		t.Fatalf("query cost failed: %v", err)
	}
	if cost < 0.49 || cost > 0.51 {
		t.Fatalf("expected cost ~0.50, got %v", cost)
	}
}

func TestSyncAll_DisabledAndUnknownTools(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	cfg := config.DefaultConfig()
	for k := range cfg.Tools {
		cfg.Tools[k] = config.ToolConfig{Enabled: false}
	}
	// Add unknown tool
	cfg.Tools["unknown_tool"] = config.ToolConfig{Enabled: true}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("expected nil error, got %v", err)
	}
	if imported != 0 {
		t.Fatalf("expected 0 imported, got %d", imported)
	}
}

func TestDaemon_PIDLifecycle(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir

	d := daemon.NewDaemon(cfg, nil, nil)
	pidPath := d.PIDFilePath()
	expectedPath := filepath.Join(tmpDir, "daemon.pid")
	if pidPath != expectedPath {
		t.Fatalf("expected %s, got %s", expectedPath, pidPath)
	}

	if err := d.WritePID(); err != nil {
		t.Fatalf("write PID failed: %v", err)
	}

	if _, err := os.Stat(pidPath); err != nil {
		t.Fatalf("PID file does not exist: %v", err)
	}

	d.RemovePID()
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatalf("expected PID file to be removed")
	}
}

func TestDaemon_StartAndCancel(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.PollInterval = 50 * time.Millisecond
	for k := range cfg.Tools {
		cfg.Tools[k] = config.ToolConfig{Enabled: false}
	}

	d := daemon.NewDaemon(cfg, st, engine)

	ctx, cancel := context.WithCancel(context.Background())
	errCh := make(chan error, 1)

	go func() {
		errCh <- d.Start(ctx)
	}()

	// Wait for PID file to appear
	pidPath := d.PIDFilePath()
	deadline := time.Now().Add(2 * time.Second)
	found := false
	for time.Now().Before(deadline) {
		if _, err := os.Stat(pidPath); err == nil {
			found = true
			break
		}
		time.Sleep(10 * time.Millisecond)
	}
	if !found {
		t.Fatal("PID file was not created in time")
	}

	// Cancel context to stop daemon
	cancel()

	select {
	case err := <-errCh:
		if err != nil {
			t.Fatalf("daemon returned error: %v", err)
		}
	case <-time.After(3 * time.Second):
		t.Fatal("daemon did not stop in time after cancellation")
	}

	// PID file should be removed
	if _, err := os.Stat(pidPath); !os.IsNotExist(err) {
		t.Fatal("PID file was not removed after daemon stop")
	}
}

func TestStopDaemon_Errors(t *testing.T) {
	// Non-existent PID file
	err := daemon.StopDaemon("/non/existent/path/daemon.pid")
	if err == nil {
		t.Fatal("expected error for non-existent PID file")
	}

	// Corrupt PID file
	tmpDir := t.TempDir()
	corruptPidFile := filepath.Join(tmpDir, "corrupt.pid")
	if err := os.WriteFile(corruptPidFile, []byte("not-a-number"), 0644); err != nil {
		t.Fatal(err)
	}
	err = daemon.StopDaemon(corruptPidFile)
	if err == nil {
		t.Fatal("expected error for invalid PID content")
	}
}

func TestDaemon_BackgroundProcessWrappers(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "test.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	// Test StopBackgroundProcess with invalid path
	err = daemon.StopBackgroundProcess(filepath.Join(tmpDir, "nonexistent.pid"))
	if err == nil {
		t.Fatal("expected error from StopBackgroundProcess on missing pid file")
	}
}
