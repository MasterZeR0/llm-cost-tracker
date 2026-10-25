package daemon_test

import (
	"context"
	"fmt"
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

func TestDaemon_FSEventDebounce(t *testing.T) {
	tmpDir := t.TempDir()
	nestedDir := filepath.Join(tmpDir, "projects", "proj-debounce")
	if err := os.MkdirAll(nestedDir, 0755); err != nil {
		t.Fatal(err)
	}

	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.WatchFSEvents = true
	cfg.Daemon.PollInterval = 10 * time.Minute
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

	time.Sleep(200 * time.Millisecond)

	sessionFile := filepath.Join(nestedDir, "session.jsonl")
	f, err := os.OpenFile(sessionFile, os.O_CREATE|os.O_WRONLY|os.O_APPEND, 0644)
	if err != nil {
		t.Fatal(err)
	}

	// Write 5 rapid events within ~100ms
	for i := 0; i < 5; i++ {
		line := `{"type":"assistant","costUSD":0.001,"durationMs":100,"message":{"model":"claude-3-haiku","role":"assistant","usage":{"input_tokens":10,"output_tokens":10,"cache_read_input_tokens":0,"cache_creation_input_tokens":0}}}` + "\n"
		if _, writeErr := f.WriteString(line); writeErr != nil {
			t.Fatal(writeErr)
		}
		time.Sleep(20 * time.Millisecond)
	}
	f.Close()

	// Immediately after writes (e.g. 100ms after last write), debounce timer (500ms) should not have fired yet
	time.Sleep(100 * time.Millisecond)
	stats, err := st.GetOverview("all")
	if err == nil && stats.TotalRequests > 0 {
		t.Logf("Note: sync completed early or caught initial write; total: %d", stats.TotalRequests)
	}

	// Wait for debounce timer to fire (500ms debounce + margin)
	var synced bool
	for i := 0; i < 20; i++ {
		time.Sleep(100 * time.Millisecond)
		stats, err := st.GetOverview("all")
		if err == nil && stats.TotalRequests == 5 {
			synced = true
			break
		}
	}

	if !synced {
		stats, _ := st.GetOverview("all")
		t.Fatalf("expected 5 requests to be synced after debouncing, got %d", stats.TotalRequests)
	}
}

func TestDaemon_WasteThresholdAlerts(t *testing.T) {
	tmpDir := t.TempDir()
	cfg := config.DefaultConfig()
	cfg.DataDir = tmpDir
	cfg.Daemon.WatchFSEvents = false
	cfg.WasteMonitoring.Enabled = true
	cfg.WasteMonitoring.SevereTurnTokenThreshold = 120000
	cfg.WasteMonitoring.WarnConsecutiveToolErrors = 3

	dbPath := filepath.Join(tmpDir, "tracker.db")
	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)
	d := daemon.NewDaemon(cfg, st, engine)

	var loggedAlerts []string
	d.SetAlertLogger(func(msg string) {
		loggedAlerts = append(loggedAlerts, msg)
	})

	// 1. Severe Turn Token Threshold
	turnsSevere := []store.Request{
		{
			ID:          "req-1",
			SessionID:   "sess-severe-1",
			Tool:        "claude_code",
			StepIndex:   1,
			InputTokens: 50000,
			Timestamp:   time.Now().Add(-10 * time.Minute),
		},
		{
			ID:          "req-2",
			SessionID:   "sess-severe-1",
			Tool:        "claude_code",
			StepIndex:   2,
			InputTokens: 135000, // > 120k threshold
			Timestamp:   time.Now().Add(-5 * time.Minute),
		},
	}

	alerts1 := d.CheckTurnWasteThresholds("sess-severe-1", turnsSevere)
	if len(alerts1) != 1 {
		t.Fatalf("expected 1 severe turn alert, got %d: %v", len(alerts1), alerts1)
	}
	expectedSub := "[DAEMON ALERT] Session 'sess-severe-1' (tool: claude_code) Turn 2: severe turn token threshold exceeded (threshold: 120000). High snowball/waste risk."
	if alerts1[0] != expectedSub {
		t.Errorf("alert mismatch.\nwant: %s\ngot:  %s", expectedSub, alerts1[0])
	}

	// 2. Consecutive Tool Errors
	turnsErrors := []store.Request{
		{
			ID:             "req-e1",
			SessionID:      "sess-errors-1",
			Tool:           "agy",
			StepIndex:      1,
			ToolErrorCount: 0,
			Timestamp:      time.Now().Add(-8 * time.Minute),
		},
		{
			ID:             "req-e2",
			SessionID:      "sess-errors-1",
			Tool:           "agy",
			StepIndex:      2,
			ToolErrorCount: 1,
			Timestamp:      time.Now().Add(-6 * time.Minute),
		},
		{
			ID:             "req-e3",
			SessionID:      "sess-errors-1",
			Tool:           "agy",
			StepIndex:      3,
			ToolErrorCount: 2,
			Timestamp:      time.Now().Add(-4 * time.Minute),
		},
		{
			ID:             "req-e4",
			SessionID:      "sess-errors-1",
			Tool:           "agy",
			StepIndex:      4,
			ToolErrorCount: 1,
			Timestamp:      time.Now().Add(-2 * time.Minute),
		},
	}

	alerts2 := d.CheckTurnWasteThresholds("sess-errors-1", turnsErrors)
	if len(alerts2) != 1 {
		t.Fatalf("expected 1 consecutive tool error alert, got %d: %v", len(alerts2), alerts2)
	}
	expectedErrSub := "[DAEMON ALERT] Session 'sess-errors-1' (tool: agy) Turn 4: consecutive tool errors (threshold: 3). High snowball/waste risk."
	if alerts2[0] != expectedErrSub {
		t.Errorf("error alert mismatch.\nwant: %s\ngot:  %s", expectedErrSub, alerts2[0])
	}

	// 3. Duplicate Deduplication / Throttling
	dupAlertsSevere := d.CheckTurnWasteThresholds("sess-severe-1", turnsSevere)
	if len(dupAlertsSevere) != 0 {
		t.Fatalf("expected 0 duplicate alerts for severe session, got %d: %v", len(dupAlertsSevere), dupAlertsSevere)
	}

	dupAlertsErrors := d.CheckTurnWasteThresholds("sess-errors-1", turnsErrors)
	if len(dupAlertsErrors) != 0 {
		t.Fatalf("expected 0 duplicate alerts for errors session, got %d: %v", len(dupAlertsErrors), dupAlertsErrors)
	}

	alerted := d.AlertedTurns()
	if alerted["sess-severe-1:severe"] != 2 {
		t.Errorf("expected alertedTurns for sess-severe-1:severe to be 2, got %d", alerted["sess-severe-1:severe"])
	}
	if alerted["sess-errors-1:tool_errors"] != 4 {
		t.Errorf("expected alertedTurns for sess-errors-1:tool_errors to be 4, got %d", alerted["sess-errors-1:tool_errors"])
	}

	// 4. Progression to Next Error Turn
	turnsErrorsWithTurn5 := append(turnsErrors, store.Request{
		ID:             "req-e5",
		SessionID:      "sess-errors-1",
		Tool:           "agy",
		StepIndex:      5,
		ToolErrorCount: 1,
		Timestamp:      time.Now(),
	})
	alertsProg := d.CheckTurnWasteThresholds("sess-errors-1", turnsErrorsWithTurn5)
	if len(alertsProg) != 1 {
		t.Fatalf("expected 1 alert for turn 5 progression, got %d: %v", len(alertsProg), alertsProg)
	}
	if !strings.Contains(alertsProg[0], "Turn 5:") {
		t.Errorf("expected alert for Turn 5, got %s", alertsProg[0])
	}

	// 5. Resolved Error Streak Does Not Alert (Trailing Success)
	turnsRecovered := []store.Request{
		{
			ID:             "req-r1",
			SessionID:      "sess-recovered",
			Tool:           "agy",
			StepIndex:      1,
			ToolErrorCount: 1,
			Timestamp:      time.Now().Add(-5 * time.Minute),
		},
		{
			ID:             "req-r2",
			SessionID:      "sess-recovered",
			Tool:           "agy",
			StepIndex:      2,
			ToolErrorCount: 1,
			Timestamp:      time.Now().Add(-4 * time.Minute),
		},
		{
			ID:             "req-r3",
			SessionID:      "sess-recovered",
			Tool:           "agy",
			StepIndex:      3,
			ToolErrorCount: 1,
			Timestamp:      time.Now().Add(-3 * time.Minute),
		},
		{
			ID:             "req-r4",
			SessionID:      "sess-recovered",
			Tool:           "agy",
			StepIndex:      4,
			ToolErrorCount: 0, // Recovered!
			Status:         "success",
			Timestamp:      time.Now().Add(-1 * time.Minute),
		},
	}
	alertsRecovered := d.CheckTurnWasteThresholds("sess-recovered", turnsRecovered)
	if len(alertsRecovered) != 0 {
		t.Errorf("expected 0 alerts for session with trailing recovery, got %d: %v", len(alertsRecovered), alertsRecovered)
	}

	// 6. Disabled Monitoring Suppresses Alerts
	cfgDisabled := config.DefaultConfig()
	cfgDisabled.WasteMonitoring.Enabled = false
	dDisabled := daemon.NewDaemon(cfgDisabled, st, engine)
	alertsDisabled := dDisabled.CheckTurnWasteThresholds("sess-severe-1", turnsSevere)
	if len(alertsDisabled) != 0 {
		t.Errorf("expected 0 alerts when waste monitoring disabled, got %d", len(alertsDisabled))
	}

	// 7. Store Integration via RunIncrementalSync
	cost := 0.05
	reqsToInsert := []store.Request{
		{
			ID:          "sync-req-1",
			SessionID:   "sess-sync-alert",
			Tool:        "claude_code",
			StepIndex:   1,
			Model:       "claude-3-5-sonnet",
			InputTokens: 140000,
			CostUSD:     &cost,
			Timestamp:   time.Now(),
		},
	}
	if _, err := st.InsertRequests(reqsToInsert); err != nil {
		t.Fatalf("failed to insert requests: %v", err)
	}

	d.ResetAlertedTurns()
	var syncAlerts []string
	d.SetAlertLogger(func(msg string) {
		syncAlerts = append(syncAlerts, msg)
	})

	if _, err := d.RunIncrementalSync(); err != nil {
		t.Fatalf("RunIncrementalSync failed: %v", err)
	}

	var foundSyncAlert bool
	for _, a := range syncAlerts {
		if strings.Contains(a, "sess-sync-alert") && strings.Contains(a, "severe turn token threshold exceeded") {
			foundSyncAlert = true
			break
		}
	}
	if !foundSyncAlert {
		t.Errorf("expected RunIncrementalSync to trigger alert for sess-sync-alert, alerts: %v", syncAlerts)
	}
}

func TestDaemon_AlertedTurnsBounding(t *testing.T) {
	cfg := config.DefaultConfig()
	cfg.WasteMonitoring.Enabled = true
	cfg.WasteMonitoring.SevereTurnTokenThreshold = 100000

	d := daemon.NewDaemon(cfg, nil, nil)

	// Process 3500 distinct sessions with severe turn token threshold breaches
	const testSessions = 3500
	for i := 0; i < testSessions; i++ {
		sessID := fmt.Sprintf("session-%d", i)
		turns := []store.Request{
			{
				ID:          fmt.Sprintf("req-%d", i),
				SessionID:   sessID,
				Tool:        "claude_code",
				StepIndex:   1,
				InputTokens: 150000,
			},
		}
		alerts := d.CheckTurnWasteThresholds(sessID, turns)
		if len(alerts) != 1 {
			t.Fatalf("expected 1 alert for session %s, got %d", sessID, len(alerts))
		}
	}

	alerted := d.AlertedTurns()
	if len(alerted) > daemon.MaxAlertedEntries {
		t.Fatalf("expected alertedTurns size to be <= %d, got %d", daemon.MaxAlertedEntries, len(alerted))
	}
	if len(alerted) == 0 {
		t.Fatal("expected alertedTurns to retain recent entries, got 0")
	}

	// Verify deduplication still works for the most recent session
	lastSessID := fmt.Sprintf("session-%d", testSessions-1)
	lastTurns := []store.Request{
		{
			ID:          "req-dup",
			SessionID:   lastSessID,
			Tool:        "claude_code",
			StepIndex:   1,
			InputTokens: 150000,
		},
	}
	dupAlerts := d.CheckTurnWasteThresholds(lastSessID, lastTurns)
	if len(dupAlerts) != 0 {
		t.Fatalf("expected 0 duplicate alerts for recent session, got %d", len(dupAlerts))
	}
}
