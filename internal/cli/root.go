package cli

import (
	"fmt"
	"os"
	"path/filepath"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/parsers"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
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
	if agyCfg, ok := cfg.Tools["agy"]; ok && agyCfg.Enabled && len(agyCfg.Paths) > 0 {
		brainPath := config.ExpandHome(agyCfg.Paths[0])
		convDir := filepath.Join(filepath.Dir(brainPath), "conversations")
		if _, statErr := os.Stat(convDir); statErr == nil {
			_, _ = parsers.BackfillAGYLatencies(st.DB(), convDir)
		}
	}
}
