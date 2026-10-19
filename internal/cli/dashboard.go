package cli

import (
	"fmt"

	"github.com/MasterZeR0/llm-cost-tracker/internal/dashboard"
	"github.com/spf13/cobra"
)

var dashboardPort int

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard server",
	PreRun: func(cmd *cobra.Command, args []string) {
		if !cmd.Flags().Changed("port") && cfg != nil && cfg.Dashboard.Port > 0 {
			dashboardPort = cfg.Dashboard.Port
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if cmd.Flags().Changed("port") && dashboardPort > 0 {
			cfg.Dashboard.Port = dashboardPort
		}
		srv := dashboard.NewServer(cfg, st)
		fmt.Printf("Dashboard server running at http://%s:%d\n", cfg.Dashboard.Host, cfg.Dashboard.Port)
		return srv.Start()
	},
}

func init() {
	dashboardCmd.Flags().IntVar(&dashboardPort, "port", 8384, "port for web dashboard")
}
