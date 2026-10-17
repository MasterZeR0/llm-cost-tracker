package daemon

import (
	"context"
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

func (d *Daemon) Start(ctx context.Context) error {
	if err := d.WritePID(); err != nil {
		return fmt.Errorf("failed to write PID: %w", err)
	}
	defer d.RemovePID()

	slog.Info("daemon started", "pid", os.Getpid())

	// Run initial sync on start
	_, _ = SyncAll(d.cfg, d.store, d.engine)

	var eventsCh <-chan fsnotify.Event
	var errorsCh <-chan error

	watcher, err := fsnotify.NewWatcher()
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
				_ = watcher.Add(expanded)
			}
			if tool.AnalyticsLog != "" {
				expanded := config.ExpandHome(tool.AnalyticsLog)
				_ = watcher.Add(expanded)
				_ = watcher.Add(filepath.Dir(expanded))
			}
		}
	} else {
		slog.Warn("failed to initialize fsnotify watcher, relying on polling only", "err", err)
	}

	pollInterval := d.cfg.Daemon.PollInterval
	if pollInterval <= 0 {
		pollInterval = 10 * time.Second
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
		return fmt.Errorf("invalid PID: %w", err)
	}

	proc, err := os.FindProcess(pid)
	if err != nil {
		return err
	}
	return proc.Signal(syscall.SIGTERM)
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
