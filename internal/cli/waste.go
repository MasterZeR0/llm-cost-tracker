package cli

import (
	"fmt"
	"io"
	"strings"
	"text/tabwriter"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
	"github.com/spf13/cobra"
)

var wastePeriodFlag string

var wasteCmd = &cobra.Command{
	Use:   "waste",
	Short: "Inspect context bloat, token waste, and session optimization diagnostics",
	PreRunE: func(cmd *cobra.Command, args []string) error {
		switch wastePeriodFlag {
		case "today", "week", "month", "all":
			return nil
		default:
			return fmt.Errorf("invalid period %q: must be one of today, week, month, all", wastePeriodFlag)
		}
	},
	RunE: func(cmd *cobra.Command, args []string) error {
		return RenderWaste(cmd.OutOrStdout(), st, wastePeriodFlag)
	},
}

// GetWasteCmdForTesting exposes wasteCmd for CLI test suites.
func GetWasteCmdForTesting() *cobra.Command {
	return wasteCmd
}

func init() {
	wasteCmd.Flags().StringVar(&wastePeriodFlag, "period", "today", "period to display: today, week, month, all")
}

// RenderWaste analyzes sessions for the specified period and outputs waste diagnostics.
func RenderWaste(w io.Writer, s *store.Store, period string, customCfg ...analyzer.Config) error {
	switch period {
	case "today", "week", "month", "all":
	default:
		return fmt.Errorf("invalid period %q: must be one of today, week, month, all", period)
	}

	details, err := s.GetPeriodSessionDetails(period)
	if err != nil {
		return fmt.Errorf("failed to fetch sessions for period %s: %w", period, err)
	}

	var analyzerCfg analyzer.Config
	if len(customCfg) > 0 {
		analyzerCfg = customCfg[0]
	} else if cfg != nil {
		analyzerCfg = analyzer.FromWasteMonitoringConfig(cfg.WasteMonitoring)
	} else {
		analyzerCfg = analyzer.DefaultConfig()
	}

	summary := analyzer.AnalyzePeriod(details, period, analyzerCfg)

	flaggedPct := 0.0
	if summary.TotalSessions > 0 {
		flaggedPct = (float64(summary.WastedSessions) / float64(summary.TotalSessions)) * 100.0
	}

	fmt.Fprintln(w, "=== LLM Cost Tracker - Context Bloat & Waste Diagnostics ===")
	fmt.Fprintf(w, "Period: %s | Sessions Analyzed: %d | Flagged: %d (%.1f%%) | Avg Health: %.1f/100\n",
		period, summary.TotalSessions, summary.WastedSessions, flaggedPct, summary.AvgHealthScore)
	fmt.Fprintf(w, "Estimated Avoidable Waste: $%.2f (%d tokens)\n",
		summary.TotalWastedUSD, summary.TotalWastedToks)

	if len(details) == 0 {
		fmt.Fprintf(w, "\nNo sessions found for period %s.\n", period)
		return nil
	}

	if summary.WastedSessions == 0 || len(summary.TopWasteSessions) == 0 {
		fmt.Fprintf(w, "\nNo context bloat or token waste detected for period %s. All sessions healthy!\n", period)
		return nil
	}

	fmt.Fprintln(w)
	tw := tabwriter.NewWriter(w, 0, 0, 2, ' ', 0)
	fmt.Fprintln(tw, "RANK\tTOOL\tSESSION ID\tCOST\tWASTE ($)\tWASTE (%)\tHEALTH\tPRIMARY ISSUE")
	for i, sess := range summary.TopWasteSessions {
		issue := primaryIssue(sess.Findings)
		fmt.Fprintf(tw, "%d\t%s\t%s\t$%.2f\t$%.2f\t%.1f%%\t%d/100\t%s\n",
			i+1, sess.Tool, sess.SessionID, sess.TotalCost, sess.WastedUSD, sess.WastePercentage, sess.HealthScore, issue)
	}
	tw.Flush()

	// List top recommendations from flagged sessions (bulleted)
	var recs []string
	seen := make(map[string]bool)
	for _, sess := range summary.TopWasteSessions {
		for _, f := range sess.Findings {
			rec := strings.TrimSpace(f.Recommendation)
			if rec != "" && !seen[rec] {
				seen[rec] = true
				recs = append(recs, rec)
			}
		}
	}

	if len(recs) > 0 {
		fmt.Fprintln(w, "\nRecommendations:")
		for _, rec := range recs {
			fmt.Fprintf(w, "  • %s\n", rec)
		}
	}

	return nil
}

func primaryIssue(findings []analyzer.WasteFinding) string {
	if len(findings) == 0 {
		return "None"
	}
	best := findings[0]
	for _, f := range findings[1:] {
		if f.WastedUSD > best.WastedUSD || (f.WastedUSD == best.WastedUSD && severityRank(f.Severity) > severityRank(best.Severity)) {
			best = f
		}
	}
	switch best.Category {
	case analyzer.WasteContextSnowball:
		return "Context Snowball"
	case analyzer.WasteCacheChurn:
		return "Cache Churn"
	case analyzer.WasteErrorLoop:
		return "Tool Error Loop"
	case analyzer.WasteRepetitiveRead:
		return "Repetitive Read"
	default:
		return string(best.Category)
	}
}

func severityRank(s analyzer.WasteSeverity) int {
	switch s {
	case analyzer.SeverityCritical:
		return 4
	case analyzer.SeverityHigh:
		return 3
	case analyzer.SeverityMedium:
		return 2
	case analyzer.SeverityLow:
		return 1
	default:
		return 0
	}
}
