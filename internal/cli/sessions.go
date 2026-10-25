package cli

import (
	"fmt"
	"io"
	"strings"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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
		default:
			return fmt.Errorf("invalid period %q: must be one of today, week, month, all", sessionsPeriodFlag)
		}
		if sessionsLimitFlag <= 0 {
			return fmt.Errorf("limit must be greater than 0, got %d", sessionsLimitFlag)
		}
		return nil
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		if len(args) > 0 {
			sessionID := strings.TrimSpace(args[0])
			if sessionID == "" {
				return fmt.Errorf("session ID cannot be empty")
			}
			return runSessionDetail(cmd.OutOrStdout(), st, sessionID)
		}
		return RenderSessionsList(cmd.OutOrStdout(), st, sessionsPeriodFlag, sessionsLimitFlag)
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
	if sec < 1.0 {
		return "<1s"
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
	if d < 24*time.Hour {
		h := int(d.Hours())
		m := int(d.Minutes()) % 60
		if m == 0 {
			return fmt.Sprintf("%dh", h)
		}
		return fmt.Sprintf("%dh %dm", h, m)
	}
	days := int(d.Hours()) / 24
	hours := int(d.Hours()) % 24
	if hours == 0 {
		return fmt.Sprintf("%dd", days)
	}
	return fmt.Sprintf("%dd %dh", days, hours)
}

func RenderSessionsList(w io.Writer, s *store.Store, period string, limit int) error {
	switch period {
	case "today", "week", "month", "all":
	default:
		return fmt.Errorf("invalid period %q: must be one of today, week, month, all", period)
	}
	if limit <= 0 {
		return fmt.Errorf("limit must be greater than 0, got %d", limit)
	}

	summaries, err := s.GetSessionSummaries(period, limit)
	if err != nil {
		return err
	}

	const innerWidth = 93
	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────────────────────────────────────╮")
	renderBoxLine(w, fmt.Sprintf("                                 Recent Sessions — %s", period), innerWidth)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	renderBoxLine(w, fmt.Sprintf(" %-18s %-12s %-6s %-10s %-8s %-8s %-9s %-14s",
		"SESSION ID", "TOOL", "TURNS", "DURATION", "TOKENS", "CACHE %", "COST", "TOOLS (ERR)"), innerWidth)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")

	if len(summaries) == 0 {
		renderBoxLine(w, " (no sessions recorded for this period)", innerWidth)
	} else {
		for _, sm := range summaries {
			displayID := sm.SessionID
			if len([]rune(displayID)) > 18 {
				displayID = string([]rune(displayID)[:15]) + "..."
			}
			toolStr := sm.Tool
			if len([]rune(toolStr)) > 12 {
				toolStr = string([]rune(toolStr)[:9]) + "..."
			}
			durStr := formatDuration(sm.DurationSeconds)
			toolInfo := fmt.Sprintf("%d", sm.ToolCallCount)
			if sm.ToolErrorCount > 0 {
				toolInfo += fmt.Sprintf(" (%d)", sm.ToolErrorCount)
			}
			cacheStr := fmt.Sprintf("%.1f%%", sm.CacheHitRate)
			costStr := fmt.Sprintf("$%.2f", sm.TotalCost)

			renderBoxLine(w, fmt.Sprintf(" %-18s %-12s %-6d %-10s %-8d %-8s %-9s %-14s",
				displayID, toolStr, sm.TurnCount, durStr, sm.TotalTokens, cacheStr, costStr, toolInfo), innerWidth)
		}
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────────────────────────────────────╯")
	return nil
}

func renderBoxWrapped(w io.Writer, prefix, text string, innerWidth int) {
	fullText := prefix + text
	if len([]rune(fullText)) <= innerWidth-2 {
		renderBoxLine(w, fullText, innerWidth)
		return
	}
	words := strings.Fields(text)
	if len(words) == 0 {
		renderBoxLine(w, prefix, innerWidth)
		return
	}
	curr := prefix
	indent := "     "
	for _, word := range words {
		testLine := curr + " " + word
		if curr == prefix {
			testLine = curr + word
		}
		if len([]rune(testLine)) <= innerWidth-2 {
			curr = testLine
		} else {
			renderBoxLine(w, curr, innerWidth)
			curr = indent + word
		}
	}
	if curr != "" && curr != indent {
		renderBoxLine(w, curr, innerWidth)
	}
}

func runSessionDetail(w io.Writer, s *store.Store, sessionID string, customCfg ...analyzer.Config) error {
	return RenderSessionDetail(w, s, sessionID, customCfg...)
}

func RenderSessionDetail(w io.Writer, s *store.Store, sessionID string, customCfg ...analyzer.Config) error {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return fmt.Errorf("session ID cannot be empty")
	}

	detail, err := s.GetSessionDetail(sessionID)
	if err != nil {
		return fmt.Errorf("session %q not found", sessionID)
	}

	var analyzerCfg analyzer.Config
	if len(customCfg) > 0 {
		analyzerCfg = customCfg[0]
	} else if cfg != nil {
		analyzerCfg = analyzer.FromWasteMonitoringConfig(cfg.WasteMonitoring)
	} else {
		analyzerCfg = analyzer.DefaultConfig()
	}
	analysis := analyzer.AnalyzeSession(*detail, analyzerCfg)

	sm := detail.Summary
	durStr := formatDuration(sm.DurationSeconds)

	const innerWidth = 93
	fmt.Fprintln(w, "╭─────────────────────────────────────────────────────────────────────────────────────────────╮")
	renderBoxLine(w, fmt.Sprintf(" Session Detail: %s", sm.SessionID), innerWidth)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	renderBoxLine(w, fmt.Sprintf(" Tool: %-12s | Turns: %-4d | Duration: %-8s | Cost: $%.2f",
		sm.Tool, sm.TurnCount, durStr, sm.TotalCost), innerWidth)
	errPart := ""
	if sm.ToolErrorCount > 0 {
		errPart = fmt.Sprintf(" (%d err)", sm.ToolErrorCount)
	}
	renderBoxLine(w, fmt.Sprintf(" Tokens: %-9d (Cache: %d Hit, %d Miss, %.1f%%) | Tools: %d%s",
		sm.TotalTokens, sm.CacheHitTokens, sm.CacheMissTokens, sm.CacheHitRate, sm.ToolCallCount, errPart), innerWidth)

	if len(analysis.Findings) > 0 || analysis.HealthScore < 100 {
		fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
		renderBoxLine(w, fmt.Sprintf(" Context Health: %d/100 (FLAGGED FOR WASTE)", analysis.HealthScore), innerWidth)
		renderBoxLine(w, fmt.Sprintf(" Waste USD:      $%.2f (%.1f%%) | Wasted Tokens: %d",
			analysis.WastedUSD, analysis.WastePercentage, analysis.WastedTokens), innerWidth)
		if len(analysis.Findings) > 0 {
			renderBoxLine(w, " Findings:", innerWidth)
			for _, f := range analysis.Findings {
				sev := strings.ToUpper(string(f.Severity))
				prefix := fmt.Sprintf("   - [%s] Turn %d: ", sev, f.TurnIndex)
				msg := fmt.Sprintf("%s -> %s", f.Explanation, f.Recommendation)
				renderBoxWrapped(w, prefix, msg, innerWidth)
			}
		}
	}
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")
	renderBoxLine(w, fmt.Sprintf(" %-5s %-20s %-20s %-8s %-34s", "STEP", "MODEL", "TOKENS (H/M/O)", "COST", "TOOLS"), innerWidth)
	fmt.Fprintln(w, "├─────────────────────────────────────────────────────────────────────────────────────────────┤")

	allZero := true
	for _, t := range detail.Turns {
		if t.StepIndex != 0 {
			allZero = false
			break
		}
	}

	for i, turn := range detail.Turns {
		stepNum := turn.StepIndex
		if allZero || stepNum == 0 {
			stepNum = int64(i + 1)
		}

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
		if len([]rune(toolsStr)) > 34 {
			toolsStr = string([]rune(toolsStr)[:31]) + "..."
		}

		modelStr := turn.Model
		if len([]rune(modelStr)) > 20 {
			modelStr = string([]rune(modelStr)[:17]) + "..."
		}

		renderBoxLine(w, fmt.Sprintf(" %-5d %-20s %-20s %-8s %-34s",
			stepNum, modelStr, tokStr, costStr, toolsStr), innerWidth)
	}

	fmt.Fprintln(w, "╰─────────────────────────────────────────────────────────────────────────────────────────────╯")
	return nil
}
