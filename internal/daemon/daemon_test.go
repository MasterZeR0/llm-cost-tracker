package daemon_test

import (
	"context"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestDaemon_DuplicatePIDDetection(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.WatchFSEvents = false

	dbPath := filepath.Join(tmpDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)
	d := daemon.NewDaemon(cfg, st, engine)

	// Simulate an active process by writing current process's PID
	currPid := os.Getpid()
	if err := os.WriteFile(d.PIDFilePath(), []byte(strconv.Itoa(currPid)), 0644); err != nil {
		t.Fatal(err)
	}

	// Starting daemon should detect that the PID is running and return an error
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	err = d.Start(ctx)
	if err == nil {
		t.Fatal("expected error starting daemon with active PID, got nil")
	}
	if !strings.Contains(err.Error(), "already running") {
		t.Fatalf("expected 'already running' error, got %v", err)
	}
}

func TestDaemon_StalePIDCleanupOnStart(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.WatchFSEvents = false

	dbPath := filepath.Join(tmpDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)
	d := daemon.NewDaemon(cfg, st, engine)

	// Write a non-existent PID (e.g. 999999)
	if err := os.WriteFile(d.PIDFilePath(), []byte("999999"), 0644); err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 200*time.Millisecond)
	defer cancel()

	// d.Start should clean up the stale PID and start cleanly
	err = d.Start(ctx)
	if err != nil && err != context.DeadlineExceeded {
		t.Fatalf("expected clean start despite stale PID, got: %v", err)
	}
}

func TestDaemon_StopDaemonStalePID(t *testing.T) {
	tmpDir := t.TempDir()
	pidPath := filepath.Join(tmpDir, "daemon.pid")

	// Write non-existent PID
	if err := os.WriteFile(pidPath, []byte("999999"), 0644); err != nil {
		t.Fatal(err)
	}

	err := daemon.StopDaemon(pidPath)
	if err == nil {
		t.Fatal("expected message/error about stale PID")
	}
	if !strings.Contains(err.Error(), "stale PID file") {
		t.Fatalf("expected 'stale PID file' mention in error, got: %v", err)
	}

	// Verify the stale PID file was deleted
	if _, statErr := os.Stat(pidPath); !os.IsNotExist(statErr) {
		t.Fatal("expected stale PID file to be removed")
	}
}

func TestDaemon_RecursiveWatch(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "projects", "proj-alpha")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.WatchFSEvents = true
	cfg.Daemon.PollInterval = 10 * time.Minute // long poll so only fsnotify triggers
	cfg.Tools = map[string]config.ToolConfig{
		"claude_code": {
			Enabled: true,
			Paths:   []string{filepath.Join(tmpDir, "projects")},
		},
	}

	dbPath := filepath.Join(tmpDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)
	d := daemon.NewDaemon(cfg, st, engine)

	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()

	go func() {
		_ = d.Start(ctx)
	}()

	// Wait for daemon to start
	time.Sleep(200 * time.Millisecond)

	// Write an event into the nested directory
	sessionFile := filepath.Join(nestedDir, "session.jsonl")
	line := `{"type":"assistant","costUSD":0.005,"durationMs":500,"message":{"model":"claude-3-haiku","role":"assistant","usage":{"input_tokens":100,"output_tokens":50,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
	if err := os.WriteFile(sessionFile, []byte(line), 0644); err != nil {
		t.Fatal(err)
	}

	// Wait up to 2 seconds for fsnotify to pick up change in nested directory
	var found bool
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		stats, err := st.GetOverview("all")
		if err == nil && stats.TotalRequests > 0 {
			found = true
			break
		}
	}

	if !found {
		t.Fatal("expected fsnotify to detect change in nested directory")
	}
}
