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
	"syscall"
	"time"

	"github.com/fsnotify/fsnotify"
	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

type Daemon struct {
	cfg    *config.Config
	store  *store.Store
	engine *pricing.Engine
}

func NewDaemon(cfg *config.Config, st *store.Store, engine *pricing.Engine) *Daemon {
	return &Daemon{
		cfg:    cfg,
		store:  st,
		engine: engine,
	}
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
	_, _ = SyncAll(d.cfg, d.store, d.engine)

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

	for {
		select {
		case <-ctx.Done():
			slog.Info("daemon stopping gracefully")
			return nil
		case <-ticker.C:
			slog.Debug("running scheduled poll sync")
			_, _ = SyncAll(d.cfg, d.store, d.engine)
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
				slog.Debug("fs event detected, syncing", "file", event.Name)
				_, _ = SyncAll(d.cfg, d.store, d.engine)
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

// StartBackgroundProcess starts the daemon synchronously with a background context.
func StartBackgroundProcess(cfg *config.Config, st *store.Store, engine *pricing.Engine) error {
	d := NewDaemon(cfg, st, engine)
	return d.Start(context.Background())
}

// StopBackgroundProcess sends SIGTERM to the daemon PID stored at pidPath.
func StopBackgroundProcess(pidPath string) error {
	return StopDaemon(pidPath)
}
