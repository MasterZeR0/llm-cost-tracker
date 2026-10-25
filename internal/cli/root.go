package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	"github.com/spf13/cobra"
)

var (
	cfgPath string
	cfg     *config.Config
	st      *store.Store
	engine  *pricing.Engine
)

var rootCmd = &cobra.Command{
	Use:   "llm-track",
	Short: "Track cost, tokens, and latency for CLI-based LLM tools",
	RunE: func(cmd *cobra.Command, args []string) error {
		return summaryCmd.RunE(cmd, args)
	},
}

func Execute() {
	if err := rootCmd.Execute(); err != nil {
		fmt.Fprintln(os.Stderr, err)
		os.Exit(1)
	}
}

func init() {
	cobra.OnInitialize(initConfig)
	rootCmd.PersistentFlags().StringVar(&cfgPath, "config", "~/.config/llm-cost-tracker/config.yaml", "config file path")

	rootCmd.AddCommand(daemonCmd)
	rootCmd.AddCommand(syncCmd)
	rootCmd.AddCommand(summaryCmd)
	rootCmd.AddCommand(sessionsCmd)
	rootCmd.AddCommand(dashboardCmd)
	rootCmd.AddCommand(toolsCmd)
	rootCmd.AddCommand(wasteCmd)
}

// SetStoreForTesting overrides the database store instance for tests.
func SetStoreForTesting(s *store.Store) {
	st = s
}

// SetConfigForTesting overrides the configuration instance for tests.
func SetConfigForTesting(c *config.Config) {
	cfg = c
}

// GetRootCmdForTesting returns the rootCmd for CLI test suites.
func GetRootCmdForTesting() *cobra.Command {
	return rootCmd
}

func initConfig() {
	var err error
	cfg, err = config.LoadConfig(cfgPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Warning: failed to load config: %v\n", err)
		cfg = config.DefaultConfig()
	}

	dbPath := filepath.Join(cfg.DataDir, "tracker.db")
	st, err = store.NewStore(dbPath)
	if err != nil {
		fmt.Fprintf(os.Stderr, "Error: failed to open store at %s: %v\n", dbPath, err)
		os.Exit(1)
	}

	engine = pricing.NewEngine(nil)
}
