package cli

import (
	"fmt"
	"io"
	"os"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
	"github.com/spf13/cobra"
)

var periodFlag string

var summaryCmd = &cobra.Command{
	Use:   "summary",
	Short: "Show cost, token, and latency summary in the terminal",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		switch periodFlag {
		case "today", "week", "month", "all":
			return nil
		default:
			return fmt.Errorf("invalid period %q: must be one of today, week, month, all", periodFlag)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return RenderSummary(os.Stdout, st, periodFlag)
	},
}

func init() {
	summaryCmd.Flags().StringVar(&periodFlag, "period", "today", "period to display: today, week, month, all")
}

func RenderSummary(w io.Writer, s *store.Store, period string) error {
	switch period {
	case "today", "week", "month", "all":
	default:
		return fmt.Errorf("invalid period %q: must be one of today, week, month, all", period)
	}

	stats, err := s.GetOverview(period)
	if err != nil {
		return err
	}

	toolUsage, _ := s.GetToolUsage(period)
	modelCost, _ := s.GetCostByModel(period)

	fmt.Fprintln(w, "╭─────────────────────────────────────────────╮")
	fmt.Fprintf(w, "│           LLM Cost Tracker — %-14s │\n", period)
	fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│  Total Requests: %-26d │\n", stats.TotalRequests)
	fmt.Fprintf(w, "│  Total Tokens:   %-26d │\n", stats.TotalTokens)
	fmt.Fprintf(w, "│  Total Cost:     $%-25.2f │\n", stats.TotalCost)
	latStr := fmt.Sprintf("%.2fs", stats.AvgLatencyMS/1000.0)
	fmt.Fprintf(w, "│  Avg Latency:    %-26s │\n", latStr)

	if len(toolUsage) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Tool:                                   │")
		for _, tu := range toolUsage {
			fmt.Fprintf(w, "│    %-12s %3d reqs   $%-6.2f          │\n", tu.Tool, tu.RequestCount, tu.TotalCost)
		}
	}

	if len(modelCost) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Model:                                  │")
		for _, mc := range modelCost {
			fmt.Fprintf(w, "│    %-16s %3d reqs  $%-6.2f       │\n", mc.Model, mc.RequestCount, mc.Cost)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────╯")
	return nil
}
