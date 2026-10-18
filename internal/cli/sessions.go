package cli

import (
	"fmt"
	"io"
	"os"
	"strings"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/store"
	"github.com/spf13/cobra"
)

var (
	sessionsPeriodFlag string
	sessionsLimitFlag  int
)

var sessionsCmd = &cobra.Command{
	Use:   "sessions [session-id]",
	Short: "List recent sessions or inspect turn-by-turn details for a session",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		switch sessionsPeriodFlag {
		case "today", "week", "month", "all":
			return nil
		default:
			return fmt.Errorf("invalid period %q: must be one of today, week, month, all", sessionsPeriodFlag)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			return RenderSessionDetail(os.Stdout, st, args[0])
		}
		return RenderSessionsList(os.Stdout, st, sessionsPeriodFlag, sessionsLimitFlag)
	},
}

func init() {
	sessionsCmd.Flags().StringVar(&sessionsPeriodFlag, "period", "today", "period to display: today, week, month, all")
	sessionsCmd.Flags().IntVar(&sessionsLimitFlag, "limit", 20, "maximum number of sessions to list")
}

func formatDuration(sec float64) string {
	if sec <= 0 {
		return "0s"
	}
	d := time.Duration(sec * float64(time.Second))
	if d < time.Minute {
		return fmt.Sprintf("%ds", int(d.Seconds()))
	}
	if d < time.Hour {
		m := int(d.Minutes())
		s := int(d.Seconds()) % 60
		if s == 0 {
			return fmt.Sprintf("%dm", m)
		}
		return fmt.Sprintf("%dm %ds", m, s)
	}
	h := int(d.Hours())
	m := int(d.Minutes()) % 60
	if m == 0 {
		return fmt.Sprintf("%dh", h)
	}
	return fmt.Sprintf("%dh %dm", h, m)
}

func RenderSessionsList(w io.Writer, s *store.Store, period string, limit int) error {
	switch period {
	case "today", "week", "month", "all":
	default:
		return fmt.Errorf("invalid period %q: must be one of today, week, month, all", period)
	}

	summaries, err := s.GetSessionSummaries(period, limit)
	if err != nil {
		return err
	}

	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────────────────────────────────────╮")
	fmt.Fprintf(w, "│                                 Recent Sessions — %-18s                │\n", period)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│ %-18s %-12s %-6s %-10s %-9s %-9s %-9s %-13s │\n",
		"SESSION ID", "TOOL", "TURNS", "DURATION", "TOKENS", "CACHE %", "COST", "TOOLS (ERR)")
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")

	if len(summaries) == 0 {
		fmt.Fprintln(w, "│ (no sessions recorded for this period)                                                      │")
	} else {
		for _, sm := range summaries {
			displayID := sm.SessionID
			if len(displayID) > 18 {
				displayID = displayID[:18]
			}
			durStr := formatDuration(sm.DurationSeconds)
			toolInfo := fmt.Sprintf("%d", sm.ToolCallCount)
			if sm.ToolErrorCount > 0 {
				toolInfo += fmt.Sprintf(" (%d)", sm.ToolErrorCount)
			}
			cacheStr := fmt.Sprintf("%.1f%%", sm.CacheHitRate)
			costStr := fmt.Sprintf("$%.2f", sm.TotalCost)

			fmt.Fprintf(w, "│ %-18s %-12s %-6d %-10s %-9d %-9s %-9s %-13s │\n",
				displayID, sm.Tool, sm.TurnCount, durStr, sm.TotalTokens, cacheStr, costStr, toolInfo)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────────────────────────────────────╯")
	return nil
}

func RenderSessionDetail(w io.Writer, s *store.Store, sessionID string) error {
	detail, err := s.GetSessionDetail(sessionID)
	if err != nil {
		return fmt.Errorf("session %q not found: %w", sessionID, err)
	}

	sm := detail.Summary
	durStr := formatDuration(sm.DurationSeconds)

	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────────────────────────────────────╮")
	fmt.Fprintf(w, "│ Session Detail: %-75s │\n", sm.SessionID)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│ Tool: %-12s | Turns: %-4d | Duration: %-8s | Cost: $%-7.2f                          │\n",
		sm.Tool, sm.TurnCount, durStr, sm.TotalCost)
	fmt.Fprintf(w, "│ Tokens: %-9d (Cache: %-7d Hit, %-7d Miss, %4.1f%%) | Tools: %-3d (%d err)          │\n",
		sm.TotalTokens, sm.CacheHitTokens, sm.CacheMissTokens, sm.CacheHitRate, sm.ToolCallCount, sm.ToolErrorCount)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	fmt.Fprintf(w, "│ %-5s %-18s %-20s %-8s %-34s │\n", "STEP", "MODEL", "TOKENS (H/M/O)", "COST", "TOOLS")
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")

	for _, turn := range detail.Turns {
		tokStr := fmt.Sprintf("%d/%d/%d", turn.CacheReadTokens, turn.InputTokens, turn.OutputTokens)
		costStr := "$0.00"
		if turn.CostUSD != nil {
			costStr = fmt.Sprintf("$%.3f", *turn.CostUSD)
		}

		var toolNames []string
		for _, tc := range turn.ToolCalls {
			name := tc.ToolName
			if tc.Status == "error" {
				name += "(err)"
			}
			toolNames = append(toolNames, name)
		}
		toolsStr := strings.Join(toolNames, ", ")
		if len(toolsStr) > 34 {
			toolsStr = toolsStr[:31] + "..."
		}

		modelStr := turn.Model
		if len(modelStr) > 18 {
			modelStr = modelStr[:18]
		}

		fmt.Fprintf(w, "│ %-5d %-18s %-20s %-8s %-34s │\n",
			turn.StepIndex, modelStr, tokStr, costStr, toolsStr)
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────────────────────────────────────╯")
	return nil
}
