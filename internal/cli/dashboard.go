package cli

import (
	"fmt"

	"github.com/nehaverma/llm-cost-tracker/internal/dashboard"
	"github.com/spf13/cobra"
)

var dashboardCmd = &cobra.Command{
	Use:   "dashboard",
	Short: "Start the web dashboard server",
	RunE: func(cmd *cobra.Command, args []string) error {
		srv := dashboard.NewServer(cfg, st)
		fmt.Printf("Dashboard server running at http://%s:%d\n", cfg.Dashboard.Host, cfg.Dashboard.Port)
		return srv.Start()
	},
}
