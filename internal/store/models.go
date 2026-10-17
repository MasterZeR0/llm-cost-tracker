package store

import "time"

type Request struct {
	ID                string     `json:"id"`
	Tool              string     `json:"tool"`
	SessionID         string     `json:"session_id"`
	Timestamp         time.Time  `json:"timestamp"`
	Model             string     `json:"model"`
	InputTokens       int64      `json:"input_tokens"`
	OutputTokens      int64      `json:"output_tokens"`
	CacheReadTokens   int64      `json:"cache_read_tokens"`
	CacheWriteTokens  int64      `json:"cache_write_tokens"`
	TotalTokens       int64      `json:"total_tokens"`
	CostUSD           *float64   `json:"cost_usd"`
	LatencyMS         *int64     `json:"latency_ms"`
	Status            string     `json:"status"`
	PromptText        string     `json:"prompt_text,omitempty"`
	ResponseText      string     `json:"response_text,omitempty"`
	SourceFile        string     `json:"source_file"`
	RawData           string     `json:"raw_data,omitempty"`
	ImportedAt        time.Time  `json:"imported_at"`
	StepIndex         int64      `json:"step_index"`
	ToolCallCount     int        `json:"tool_call_count"`
	ToolErrorCount    int        `json:"tool_error_count"`
	SubagentCallCount int        `json:"subagent_call_count"`
	ToolCalls         []ToolCall `json:"tool_calls,omitempty"`
}

type ToolCall struct {
	ID        string    `json:"id"`
	RequestID string    `json:"request_id"`
	SessionID string    `json:"session_id"`
	ToolName  string    `json:"tool_name"`
	Status    string    `json:"status"`
	CreatedAt time.Time `json:"created_at"`
}

type SessionSummary struct {
	SessionID       string    `json:"session_id"`
	Tool            string    `json:"tool"`
	StartTime       time.Time `json:"start_time"`
	EndTime         time.Time `json:"end_time"`
	DurationSeconds float64   `json:"duration_seconds"`
	TurnCount       int64     `json:"turn_count"`
	TotalTokens     int64     `json:"total_tokens"`
	CacheHitTokens  int64     `json:"cache_hit_tokens"`
	CacheMissTokens int64     `json:"cache_miss_tokens"`
	CacheHitRate    float64   `json:"cache_hit_rate"`
	TotalCost       float64   `json:"total_cost"`
	ToolCallCount   int64     `json:"tool_call_count"`
	ToolErrorCount  int64     `json:"tool_error_count"`
	SubagentCount   int64     `json:"subagent_count"`
}

type SessionDetail struct {
	Summary SessionSummary `json:"summary"`
	Turns   []Request      `json:"turns"`
}

type CacheStats struct {
	CacheHitTokens  int64   `json:"cache_hit_tokens"`
	CacheMissTokens int64   `json:"cache_miss_tokens"`
	OutputTokens    int64   `json:"output_tokens"`
	TotalPrompt     int64   `json:"total_prompt_tokens"`
	CacheHitRate    float64 `json:"cache_hit_rate"`
	SavingsUSD      float64 `json:"savings_usd"`
}

type ToolCallStat struct {
	ToolName   string `json:"tool_name"`
	TotalCalls int64  `json:"total_calls"`
	ErrorCount int64  `json:"error_count"`
}

type SyncState struct {
	SourcePath   string     `json:"source_path"`
	Tool         string     `json:"tool"`
	LastOffset   int64      `json:"last_offset"`
	LastModified *time.Time `json:"last_modified"`
	LastRowID    string     `json:"last_row_id"`
	UpdatedAt    time.Time  `json:"updated_at"`
}

type PricingModel struct {
	ModelPattern      string    `json:"model_pattern"`
	InputPerMTok      float64   `json:"input_per_mtok"`
	OutputPerMTok     float64   `json:"output_per_mtok"`
	CacheReadPerMTok  float64   `json:"cache_read_per_mtok"`
	CacheWritePerMTok float64   `json:"cache_write_per_mtok"`
	EffectiveDate     time.Time `json:"effective_date"`
	Source            string    `json:"source"`
}

type OverviewStats struct {
	TotalRequests      int64   `json:"total_requests"`
	TotalSessions      int64   `json:"total_sessions"`
	AvgTurnsPerSession float64 `json:"avg_turns_per_session"`
	TotalTokens        int64   `json:"total_tokens"`
	CacheHitTokens     int64   `json:"cache_hit_tokens"`
	CacheMissTokens    int64   `json:"cache_miss_tokens"`
	CacheHitRate       float64 `json:"cache_hit_rate"`
	CacheSavingsUSD    float64 `json:"cache_savings_usd"`
	TotalCost          float64 `json:"total_cost"`
	AvgLatencyMS       float64 `json:"avg_latency_ms"`
	TotalToolCalls     int64   `json:"total_tool_calls"`
	TotalToolErrors    int64   `json:"total_tool_errors"`
	TotalSubagents     int64   `json:"total_subagents"`
}

