package cli

import (
	"context"
	"fmt"
	"os"
	"os/signal"
	"path/filepath"
	"strings"
	"syscall"

	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
	"github.com/spf13/cobra"
)

var daemonCmd = &cobra.Command{
	Use:   "daemon",
	Short: "Manage the background watcher daemon",
}

var daemonStartCmd = &cobra.Command{
	Use:   "start",
	Short: "Start the background watcher daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		d := daemon.NewDaemon(cfg, st, engine)
		if _, err := d.CheckRunningPID(); err != nil {
			return err
		}

		ctx, cancel := context.WithCancel(context.Background())
		defer cancel()

		sigCh := make(chan os.Signal, 1)
		signal.Notify(sigCh, os.Interrupt, syscall.SIGTERM)
		go func() {
			<-sigCh
			cancel()
		}()

		fmt.Printf("Starting LLM Cost Tracker daemon (PID %d)...\n", os.Getpid())
		return d.Start(ctx)
	},
}

var daemonStopCmd = &cobra.Command{
	Use:   "stop",
	Short: "Stop the running background daemon",
	RunE: func(cmd *cobra.Command, args []string) error {
		pidFile := filepath.Join(cfg.DataDir, "daemon.pid")
		if err := daemon.StopDaemon(pidFile); err != nil {
			if strings.Contains(err.Error(), "removed stale PID file") {
				fmt.Println(err.Error())
				return nil
			}
			return fmt.Errorf("failed to stop daemon: %w", err)
		}
		fmt.Println("Daemon stopped successfully.")
		return nil
	},
}

func init() {
	daemonCmd.AddCommand(daemonStartCmd)
	daemonCmd.AddCommand(daemonStopCmd)
}
