package cli

import (
	"fmt"
	"io"
	"strings"

	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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
		return RenderSummary(cmd.OutOrStdout(), st, periodFlag)
	},
}

func init() {
	summaryCmd.Flags().StringVar(&periodFlag, "period", "today", "period to display: today, week, month, all")
}

func renderBoxLine(w io.Writer, content string, innerWidth int) {
	runes := []rune(content)
	if len(runes) > innerWidth {
		runes = runes[:innerWidth]
	}
	padding := innerWidth - len(runes)
	if padding < 0 {
		padding = 0
	}
	fmt.Fprintf(w, "│%s%s│\n", string(runes), strings.Repeat(" ", padding))
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

	const innerWidth = 61
	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────╮")
	renderBoxLine(w, fmt.Sprintf("                   LLM Cost Tracker — %s", period), innerWidth)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
	renderBoxLine(w, fmt.Sprintf("  Total Sessions:    %-39d", stats.TotalSessions), innerWidth)
	turnsStr := fmt.Sprintf("%d (avg %.1f / session)", stats.TotalRequests, stats.AvgTurnsPerSession)
	renderBoxLine(w, fmt.Sprintf("  Total Turns:       %-39s", turnsStr), innerWidth)
	renderBoxLine(w, fmt.Sprintf("  Total Tokens:      %-39d", stats.TotalTokens), innerWidth)
	cacheStr := fmt.Sprintf("%.1f%% ($%.2f saved)", stats.CacheHitRate, stats.CacheSavingsUSD)
	renderBoxLine(w, fmt.Sprintf("  Cache Hit Rate:    %-39s", cacheStr), innerWidth)
	renderBoxLine(w, fmt.Sprintf("  Total Cost:        $%-38.2f", stats.TotalCost), innerWidth)
	latStr := fmt.Sprintf("%.2fs", stats.AvgLatencyMS/1000.0)
	renderBoxLine(w, fmt.Sprintf("  Avg Latency:       %-39s", latStr), innerWidth)

	if len(toolCallStats) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		renderBoxLine(w, "  Top Agentic Tools:", innerWidth)
		limit := len(toolCallStats)
		if limit > 4 {
			limit = 4
		}
		for i := 0; i < limit; i++ {
			tc := toolCallStats[i]
			toolName := tc.ToolName
			if len([]rune(toolName)) > 18 {
				toolName = string([]rune(toolName)[:15]) + "..."
			}
			errPart := ""
			if tc.ErrorCount > 0 {
				errPart = fmt.Sprintf(" (%d errors)", tc.ErrorCount)
			}
			callInfo := fmt.Sprintf("%d calls%s", tc.TotalCalls, errPart)
			renderBoxLine(w, fmt.Sprintf("    %-18s %s", toolName, callInfo), innerWidth)
		}
	}

	if len(toolUsage) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		renderBoxLine(w, "  By Tool:", innerWidth)
		for _, tu := range toolUsage {
			toolName := tu.Tool
			if len([]rune(toolName)) > 14 {
				toolName = string([]rune(toolName)[:11]) + "..."
			}
			renderBoxLine(w, fmt.Sprintf("    %-14s %5d turns    $%10.2f", toolName, tu.RequestCount, tu.TotalCost), innerWidth)
		}
	}

	if len(modelCost) > 0 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────┤")
		renderBoxLine(w, "  By Model:", innerWidth)
		for _, mc := range modelCost {
			modelName := mc.Model
			if len([]rune(modelName)) > 22 {
				modelName = string([]rune(modelName)[:19]) + "..."
			}
			renderBoxLine(w, fmt.Sprintf("    %-22s %5d turns    $%10.2f", modelName, mc.RequestCount, mc.Cost), innerWidth)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────╯")
	return nil
}
