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
	toolCallStats, _ := s.GetToolCallStats(period)

	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────╮")
	fmt.Fprintf(w, "│                   LLM Cost Tracker — %-22s │\n", period)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│  Total Sessions:    %-39d │\n", stats.TotalSessions)
	turnsStr := fmt.Sprintf("%d (avg %.1f / session)", stats.TotalRequests, stats.AvgTurnsPerSession)
	fmt.Fprintf(w, "│  Total Turns:       %-39s │\n", turnsStr)
	fmt.Fprintf(w, "│  Total Tokens:      %-39d │\n", stats.TotalTokens)
	cacheStr := fmt.Sprintf("%.1f%% ($%.2f saved)", stats.CacheHitRate, stats.CacheSavingsUSD)
	fmt.Fprintf(w, "│  Cache Hit Rate:    %-39s │\n", cacheStr)
	fmt.Fprintf(w, "│  Total Cost:        $%-38.2f │\n", stats.TotalCost)
	latStr := fmt.Sprintf("%.2fs", stats.AvgLatencyMS/1000.0)
	fmt.Fprintf(w, "│  Avg Latency:       %-39s │\n", latStr)

	if len(toolCallStats) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  Top Agentic Tools:                                         │")
		limit := len(toolCallStats)
		if limit > 4 {
			limit = 4
		}
		for i := 0; i < limit; i++ {
			tc := toolCallStats[i]
			errPart := ""
			if tc.ErrorCount > 0 {
				errPart = fmt.Sprintf(" (%d errors)", tc.ErrorCount)
			}
			callInfo := fmt.Sprintf("%d calls%s", tc.TotalCalls, errPart)
			fmt.Fprintf(w, "│    %-18s %-36s │\n", tc.ToolName, callInfo)
		}
	}

	if len(toolUsage) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Tool:                                                   │")
		for _, tu := range toolUsage {
			fmt.Fprintf(w, "│    %-12s %3d turns    $%-23.2f │\n", tu.Tool, tu.RequestCount, tu.TotalCost)
		}
	}


	if len(modelCost) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		fmt.Fprintln(w, "│  By Model:                                                  │")
		for _, mc := range modelCost {
			fmt.Fprintf(w, "│    %-16s %3d turns   $%-23.2f │\n", mc.Model, mc.RequestCount, mc.Cost)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────╯")
	return nil

}
