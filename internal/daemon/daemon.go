package daemon

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"syscall"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	"github.com/fsnotify/fsnotify"
)

type Daemon struct {
	cfg          *config.Config
	store        *store.Store
	engine       *pricing.Engine
	alertMu      sync.Mutex
	alertedTurns map[string]int64
	alertLogger  func(msg string)
}

func NewDaemon(cfg *config.Config, st *store.Store, engine *pricing.Engine) *Daemon {
	return &Daemon{
		cfg:          cfg,
		store:        st,
		engine:       engine,
		alertedTurns: make(map[string]int64),
	}
}

// SetAlertLogger sets a custom alert logging callback (useful for testing or custom notification pipelines).
func (d *Daemon) SetAlertLogger(fn func(msg string)) {
	d.alertMu.Lock()
	defer d.alertMu.Unlock()
	d.alertLogger = fn
}

// AlertedTurns returns a copy of the recorded alert turn checkpoints.
func (d *Daemon) AlertedTurns() map[string]int64 {
	d.alertMu.Lock()
	defer d.alertMu.Unlock()
	res := make(map[string]int64, len(d.alertedTurns))
	for k, v := range d.alertedTurns {
		res[k] = v
	}
	return res
}

// ResetAlertedTurns clears the recorded alert turn checkpoints.
func (d *Daemon) ResetAlertedTurns() {
	d.alertMu.Lock()
	defer d.alertMu.Unlock()
	d.alertedTurns = make(map[string]int64)
}

func (d *Daemon) logAlert(msg string) {
	d.alertMu.Lock()
	logger := d.alertLogger
	d.alertMu.Unlock()

	if logger != nil {
		logger(msg)
	}
	slog.Warn(msg)
	if d.cfg != nil && d.cfg.WasteMonitoring.LogAlertsToConsole {
		fmt.Println(msg)
	}
}

// CheckTurnWasteThresholds inspects session turns for waste violations:
// Check 1: Severe turn token threshold breaches.
// Check 2: Consecutive trailing tool execution errors.
// Triggered alerts are logged and returned; duplicates on subsequent calls are throttled.
func (d *Daemon) CheckTurnWasteThresholds(sessionID string, turns []store.Request) []string {
	if d.cfg == nil || !d.cfg.WasteMonitoring.Enabled || len(turns) == 0 {
		return nil
	}

	var alerts []string

	d.alertMu.Lock()
	if d.alertedTurns == nil {
		d.alertedTurns = make(map[string]int64)
	}

	tool := "unknown"
	for _, t := range turns {
		if t.Tool != "" {
			tool = t.Tool
			break
		}
	}

	severeThreshold := d.cfg.WasteMonitoring.SevereTurnTokenThreshold
	if severeThreshold <= 0 {
		severeThreshold = d.cfg.WasteMonitoring.SnowballTokenThreshold
	}
	if severeThreshold <= 0 {
		severeThreshold = 120000
	}

	snowballThreshold := d.cfg.WasteMonitoring.SnowballTokenThreshold
	if snowballThreshold <= 0 {
		snowballThreshold = 80000
	}

	// Check 1: Severe turn token threshold on any turn in the active session
	for i, turn := range turns {
		turnNum := turn.StepIndex
		if turnNum == 0 {
			turnNum = int64(i + 1)
		}

		tTool := turn.Tool
		if tTool == "" {
			tTool = tool
		}

		if turn.InputTokens >= severeThreshold {
			key := fmt.Sprintf("%s:severe", sessionID)
			if lastAlerted, exists := d.alertedTurns[key]; !exists || turnNum > lastAlerted {
				d.alertedTurns[key] = turnNum
				reason := "severe turn token threshold exceeded"
				msg := fmt.Sprintf("[DAEMON ALERT] Session '%s' (tool: %s) Turn %d: %s (threshold: %d). High snowball/waste risk.",
					sessionID, tTool, turnNum, reason, severeThreshold)
				alerts = append(alerts, msg)
			}
		} else if d.cfg.WasteMonitoring.SnowballTokenThreshold > 0 && turn.InputTokens >= snowballThreshold {
			key := fmt.Sprintf("%s:snowball", sessionID)
			if lastAlerted, exists := d.alertedTurns[key]; !exists || turnNum > lastAlerted {
				d.alertedTurns[key] = turnNum
				reason := "snowball token threshold exceeded"
				msg := fmt.Sprintf("[DAEMON ALERT] Session '%s' (tool: %s) Turn %d: %s (threshold: %d). High snowball/waste risk.",
					sessionID, tTool, turnNum, reason, snowballThreshold)
				alerts = append(alerts, msg)
			}
		}
	}

	// Check 2: Consecutive tool errors in trailing turns
	warnConsecutiveErrors := d.cfg.WasteMonitoring.WarnConsecutiveToolErrors
	if warnConsecutiveErrors <= 0 {
		warnConsecutiveErrors = 3
	}

	consecutiveErrors := 0
	for i := len(turns) - 1; i >= 0; i-- {
		if turns[i].ToolErrorCount > 0 || strings.EqualFold(turns[i].Status, "error") || strings.EqualFold(turns[i].Status, "failed") {
			consecutiveErrors++
		} else {
			break
		}
	}

	if consecutiveErrors >= warnConsecutiveErrors {
		lastTurn := turns[len(turns)-1]
		turnNum := lastTurn.StepIndex
		if turnNum == 0 {
			turnNum = int64(len(turns))
		}
		tTool := lastTurn.Tool
		if tTool == "" {
			tTool = tool
		}

		key := fmt.Sprintf("%s:tool_errors", sessionID)
		if lastAlerted, exists := d.alertedTurns[key]; !exists || turnNum > lastAlerted {
			d.alertedTurns[key] = turnNum
			reason := "consecutive tool errors"
			msg := fmt.Sprintf("[DAEMON ALERT] Session '%s' (tool: %s) Turn %d: %s (threshold: %d). High snowball/waste risk.",
				sessionID, tTool, turnNum, reason, warnConsecutiveErrors)
			alerts = append(alerts, msg)
		}
	}
	d.alertMu.Unlock()

	for _, msg := range alerts {
		d.logAlert(msg)
	}

	return alerts
}

func (d *Daemon) checkActiveSessions() {
	if d.store == nil {
		return
	}
	summaries, err := d.store.GetSessionSummaries("all", 10)
	if err != nil || len(summaries) == 0 {
		return
	}
	for _, sm := range summaries {
		detail, err := d.store.GetSessionDetail(sm.SessionID)
		if err != nil || detail == nil || len(detail.Turns) == 0 {
			continue
		}
		d.CheckTurnWasteThresholds(sm.SessionID, detail.Turns)
	}
}

// RunIncrementalSync executes SyncAll and evaluates active sessions for waste threshold breaches.
func (d *Daemon) RunIncrementalSync() (int, error) {
	imported, err := SyncAll(d.cfg, d.store, d.engine)
	if d.cfg != nil && d.cfg.WasteMonitoring.Enabled && d.store != nil {
		d.checkActiveSessions()
	}
	return imported, err
}

func (d *Daemon) PIDFilePath() string {
	return filepath.Join(d.cfg.DataDir, "daemon.pid")
}

func (d *Daemon) CheckRunningPID() (int, error) {
	data, err := os.ReadFile(d.PIDFilePath())
	if err != nil {
		if os.IsNotExist(err) {
			return 0, nil
		}
		return 0, err
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil || pid <= 0 {
		_ = os.Remove(d.PIDFilePath())
		return 0, nil
	}

	err = syscall.Kill(pid, 0)
	if err == nil || errors.Is(err, syscall.EPERM) {
		return pid, fmt.Errorf("daemon is already running (PID %d)", pid)
	}
	if errors.Is(err, syscall.ESRCH) {
		// Process is dead, clean up stale PID file
		_ = os.Remove(d.PIDFilePath())
		return 0, nil
	}
	return 0, nil
}

func (d *Daemon) WritePID() error {
	if err := os.MkdirAll(d.cfg.DataDir, 0755); err != nil {
		return err
	}
	pid := os.Getpid()
	return os.WriteFile(d.PIDFilePath(), []byte(strconv.Itoa(pid)), 0644)
}

func (d *Daemon) RemovePID() {
	_ = os.Remove(d.PIDFilePath())
}

func watchRecursive(watcher *fsnotify.Watcher, root string) error {
	info, err := os.Stat(root)
	if err != nil {
		if os.IsNotExist(err) {
			parent := filepath.Dir(root)
			if pInfo, pErr := os.Stat(parent); pErr == nil && pInfo.IsDir() {
				_ = watcher.Add(parent)
			}
		}
		return err
	}
	if !info.IsDir() {
		_ = watcher.Add(root)
		_ = watcher.Add(filepath.Dir(root))
		return nil
	}

	return filepath.Walk(root, func(path string, fi os.FileInfo, err error) error {
		if err != nil {
			return nil
		}
		if fi.IsDir() {
			_ = watcher.Add(path)
		}
		return nil
	})
}

func (d *Daemon) Start(ctx context.Context) error {
	if _, err := d.CheckRunningPID(); err != nil {
		return err
	}
	if err := d.WritePID(); err != nil {
		return fmt.Errorf("failed to write PID: %w", err)
	}
	defer d.RemovePID()

	slog.Info("daemon started", "pid", os.Getpid())

	// Run initial sync on start
	_, _ = d.RunIncrementalSync()

	var eventsCh <-chan fsnotify.Event
	var errorsCh <-chan error
	var watcher *fsnotify.Watcher

	if d.cfg.Daemon.WatchFSEvents {
		var err error
		watcher, err = fsnotify.NewWatcher()
		if err == nil {
			defer watcher.Close()
			eventsCh = watcher.Events
			errorsCh = watcher.Errors

			for _, tool := range d.cfg.Tools {
				if !tool.Enabled {
					continue
				}
				for _, p := range tool.Paths {
					expanded := config.ExpandHome(p)
					_ = watchRecursive(watcher, expanded)
				}
				if tool.AnalyticsLog != "" {
					expanded := config.ExpandHome(tool.AnalyticsLog)
					_ = watchRecursive(watcher, expanded)
				}
			}
		} else {
			slog.Warn("failed to initialize fsnotify watcher, relying on polling only", "err", err)
		}
	}

	pollInterval := d.cfg.Daemon.PollInterval
	if pollInterval <= 0 {
		pollInterval = 30 * time.Second
	}
	ticker := time.NewTicker(pollInterval)
	defer ticker.Stop()

	const debounceDuration = 500 * time.Millisecond
	var debounceTimer *time.Timer
	var debounceCh <-chan time.Time
	defer func() {
		if debounceTimer != nil {
			debounceTimer.Stop()
		}
	}()

	for {
		select {
		case <-ctx.Done():
			slog.Info("daemon stopping gracefully")
			return nil
		case <-ticker.C:
			slog.Debug("running scheduled poll sync")
			if debounceTimer != nil {
				if !debounceTimer.Stop() {
					select {
					case <-debounceTimer.C:
					default:
					}
				}
				debounceTimer = nil
				debounceCh = nil
			}
			_, _ = d.RunIncrementalSync()
		case <-debounceCh:
			slog.Debug("debounced fs sync triggering")
			debounceTimer = nil
			debounceCh = nil
			_, _ = d.RunIncrementalSync()
		case event, ok := <-eventsCh:
			if !ok {
				eventsCh = nil
				continue
			}
			if event.Has(fsnotify.Create) {
				if watcher != nil {
					if fi, err := os.Stat(event.Name); err == nil && fi.IsDir() {
						_ = watchRecursive(watcher, event.Name)
					}
				}
			}
			if event.Has(fsnotify.Write) || event.Has(fsnotify.Create) {
				slog.Debug("fs event detected, debouncing sync", "file", event.Name)
				if debounceTimer == nil {
					debounceTimer = time.NewTimer(debounceDuration)
					debounceCh = debounceTimer.C
				} else {
					if !debounceTimer.Stop() {
						select {
						case <-debounceTimer.C:
						default:
						}
					}
					debounceTimer.Reset(debounceDuration)
				}
			}
		case wErr, ok := <-errorsCh:
			if !ok {
				errorsCh = nil
				continue
			}
			slog.Warn("fs watcher error", "err", wErr)
		}
	}
}

func StopDaemon(pidPath string) error {
	data, err := os.ReadFile(pidPath)
	if err != nil {
		return fmt.Errorf("could not read PID file: %w", err)
	}
	pid, err := strconv.Atoi(strings.TrimSpace(string(data)))
	if err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("invalid PID in %s; removed corrupt PID file", pidPath)
	}

	// Check if process is alive before signaling
	err = syscall.Kill(pid, 0)
	if err != nil {
		if errors.Is(err, syscall.ESRCH) {
			_ = os.Remove(pidPath)
			return fmt.Errorf("daemon process (PID %d) is not running; removed stale PID file", pid)
		}
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		_ = os.Remove(pidPath)
		return fmt.Errorf("failed to find daemon process (PID %d): %w; removed stale PID file", pid, err)
	}

	err = proc.Signal(syscall.SIGTERM)
	if err != nil {
		if errors.Is(err, os.ErrProcessDone) || errors.Is(err, syscall.ESRCH) || strings.Contains(err.Error(), "process already finished") {
			_ = os.Remove(pidPath)
			return fmt.Errorf("daemon process (PID %d) is not running; removed stale PID file", pid)
		}
		return fmt.Errorf("failed to send SIGTERM to process (PID %d): %w", pid, err)
	}

	// Wait up to 1 second for process to cleanly exit and remove PID file
	for i := 0; i < 10; i++ {
		time.Sleep(100 * time.Millisecond)
		if err := syscall.Kill(pid, 0); err != nil {
			_ = os.Remove(pidPath)
			return nil
		}
	}

	return nil
}
