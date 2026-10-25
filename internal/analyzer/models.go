package analyzer

import (
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
)

// WasteCategory defines the classification of token or context waste.
type WasteCategory string

const (
	WasteContextSnowball WasteCategory = "context_snowball"
	WasteCacheChurn      WasteCategory = "cache_churn"
	WasteErrorLoop       WasteCategory = "tool_error_loop"
	WasteRepetitiveRead  WasteCategory = "repetitive_read"
)

// WasteSeverity represents the impact severity of a waste finding.
type WasteSeverity string

const (
	SeverityLow      WasteSeverity = "low"
	SeverityMedium   WasteSeverity = "medium"
	SeverityHigh     WasteSeverity = "high"
	SeverityCritical WasteSeverity = "critical"
)

// WasteFinding records a specific occurrence or pattern of token/context waste.
type WasteFinding struct {
	Category       WasteCategory `json:"category"`
	Severity       WasteSeverity `json:"severity"`
	TurnIndex      int64         `json:"turn_index"`
	WastedTokens   int64         `json:"wasted_tokens"`
	WastedUSD      float64       `json:"wasted_usd"`
	Explanation    string        `json:"explanation"`
	Recommendation string        `json:"recommendation"`
}

// TurnMetric captures token usage, cost, and waste classification for an individual session turn.
type TurnMetric struct {
	StepIndex       int64   `json:"step_index"`
	InputTokens     int64   `json:"input_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	CacheReadTokens int64   `json:"cache_read_tokens"`
	CostUSD         float64 `json:"cost_usd"`
	HasToolError    bool    `json:"has_tool_error"`
	IsWaste         bool    `json:"is_waste"`
}

// SessionWasteAnalysis contains the complete waste diagnosis and trajectory for a single session.
type SessionWasteAnalysis struct {
	SessionID       string         `json:"session_id"`
	Tool            string         `json:"tool"`
	TotalTokens     int64          `json:"total_tokens"`
	TotalCost       float64        `json:"total_cost"`
	WastedTokens    int64          `json:"wasted_tokens"`
	WastedUSD       float64        `json:"wasted_usd"`
	WastePercentage float64        `json:"waste_percentage"`
	HealthScore     int            `json:"health_score"` // 0-100 (100 = optimal, 0 = critical waste)
	Findings        []WasteFinding `json:"findings"`
	Trajectory      []TurnMetric   `json:"trajectory"`
}

// PeriodWasteSummary aggregates waste findings across all sessions in a given time period.
type PeriodWasteSummary struct {
	Period           string                    `json:"period"`
	TotalSessions    int64                     `json:"total_sessions"`
	WastedSessions   int64                     `json:"wasted_sessions"`
	TotalWastedUSD   float64                   `json:"total_wasted_usd"`
	TotalWastedToks  int64                     `json:"total_wasted_tokens"`
	AvgHealthScore   float64                   `json:"avg_health_score"`
	TopWasteSessions []SessionWasteAnalysis    `json:"top_waste_sessions"`
	CategoryTotals   map[WasteCategory]float64 `json:"category_totals_usd"`
}

// Config defines the configuration thresholds for waste detection heuristics.
type Config struct {
	Enabled                   bool    `json:"enabled" yaml:"enabled"`
	SnowballTokenThreshold    int64   `json:"snowball_token_threshold" yaml:"snowball_token_threshold"`
	SevereTurnTokenThreshold  int64   `json:"severe_turn_token_threshold" yaml:"severe_turn_token_threshold"`
	WarnConsecutiveToolErrors int     `json:"warn_consecutive_tool_errors" yaml:"warn_consecutive_tool_errors"`
	CacheChurnDropThreshold   float64 `json:"cache_churn_drop_threshold" yaml:"cache_churn_drop_threshold"`
}

// DefaultConfig returns an analyzer Config with sensible default thresholds matching the design spec.
func DefaultConfig() Config {
	return Config{
		Enabled:                   true,
		SnowballTokenThreshold:    80000,
		SevereTurnTokenThreshold:  120000,
		WarnConsecutiveToolErrors: 3,
		CacheChurnDropThreshold:   0.40,
	}
}

// FromWasteMonitoringConfig converts a config.WasteMonitoringConfig into an analyzer Config.
func FromWasteMonitoringConfig(cfg config.WasteMonitoringConfig) Config {
	return Config{
		Enabled:                   cfg.Enabled,
		SnowballTokenThreshold:    cfg.SnowballTokenThreshold,
		SevereTurnTokenThreshold:  cfg.SevereTurnTokenThreshold,
		WarnConsecutiveToolErrors: cfg.WarnConsecutiveToolErrors,
		CacheChurnDropThreshold:   cfg.CacheChurnDropThreshold,
	}
}
