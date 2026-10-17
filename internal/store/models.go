package store

import "time"

type Request struct {
	ID               string    `json:"id"`
	Tool             string    `json:"tool"`
	SessionID        string    `json:"session_id"`
	Timestamp        time.Time `json:"timestamp"`
	Model            string    `json:"model"`
	InputTokens      int64     `json:"input_tokens"`
	OutputTokens     int64     `json:"output_tokens"`
	CacheReadTokens  int64     `json:"cache_read_tokens"`
	CacheWriteTokens int64     `json:"cache_write_tokens"`
	TotalTokens      int64     `json:"total_tokens"`
	CostUSD          *float64  `json:"cost_usd"`
	LatencyMS        *int64    `json:"latency_ms"`
	Status           string    `json:"status"`
	PromptText       string    `json:"prompt_text,omitempty"`
	ResponseText     string    `json:"response_text,omitempty"`
	SourceFile       string    `json:"source_file"`
	RawData          string    `json:"raw_data,omitempty"`
	ImportedAt       time.Time `json:"imported_at"`
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
	TotalRequests int64   `json:"total_requests"`
	TotalTokens   int64   `json:"total_tokens"`
	TotalCost     float64 `json:"total_cost"`
	AvgLatencyMS  float64 `json:"avg_latency_ms"`
}
