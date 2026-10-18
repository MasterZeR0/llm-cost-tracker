package store

import (
	"database/sql"
	"fmt"
	"strings"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
)


type TimeSeriesPoint struct {
	Date string  `json:"date"`
	Tool string  `json:"tool"`
	Cost float64 `json:"cost"`
}

type ModelCostPoint struct {
	Model        string  `json:"model"`
	Cost         float64 `json:"cost"`
	RequestCount int64   `json:"request_count"`
}

type ModelTokenPoint struct {
	Model        string `json:"model"`
	InputTokens  int64  `json:"input_tokens"`
	OutputTokens int64  `json:"output_tokens"`
}

type ToolUsagePoint struct {
	Tool         string  `json:"tool"`
	RequestCount int64   `json:"request_count"`
	TotalCost    float64 `json:"total_cost"`
}

func periodClause(period string) string {
	switch period {
	case "today":
		return "WHERE timestamp >= date('now', 'start of day')"
	case "week":
		return "WHERE timestamp >= date('now', '-7 days')"
	case "month":
		return "WHERE timestamp >= date('now', '-30 days')"
	default:
		return ""
	}
}

func (s *Store) GetOverview(period string) (*OverviewStats, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT 
			count(*),
			count(distinct session_id),
			coalesce(sum(total_tokens), 0),
			coalesce(sum(input_tokens), 0),
			coalesce(sum(cache_read_tokens), 0),
			coalesce(sum(cost_usd), 0.0),
			coalesce(avg(latency_ms), 0.0),
			coalesce(sum(tool_call_count), 0),
			coalesce(sum(tool_error_count), 0),
			coalesce(sum(subagent_call_count), 0)
		FROM requests %s
	`, clause)

	var stats OverviewStats
	err := s.db.QueryRow(query).Scan(
		&stats.TotalRequests,
		&stats.TotalSessions,
		&stats.TotalTokens,
		&stats.CacheMissTokens,
		&stats.CacheHitTokens,
		&stats.TotalCost,
		&stats.AvgLatencyMS,
		&stats.TotalToolCalls,
		&stats.TotalToolErrors,
		&stats.TotalSubagents,
	)
	if err != nil {
		return nil, err
	}

	if stats.TotalSessions > 0 {
		stats.AvgTurnsPerSession = float64(stats.TotalRequests) / float64(stats.TotalSessions)
	}

	promptTotal := stats.CacheHitTokens + stats.CacheMissTokens
	if promptTotal > 0 {
		stats.CacheHitRate = (float64(stats.CacheHitTokens) / float64(promptTotal)) * 100.0
	}

	savings, err := s.calculateCacheSavings(clause)
	if err == nil {
		stats.CacheSavingsUSD = savings
	}

	return &stats, nil
}

func (s *Store) getPricingEngine() *pricing.Engine {
	customPrices := make(map[string]pricing.ModelPrice)
	rows, err := s.db.Query(`SELECT model_pattern, input_per_mtok, output_per_mtok, cache_read_per_mtok, cache_write_per_mtok FROM pricing`)
	if err == nil {
		defer rows.Close()
		for rows.Next() {
			var pat string
			var p pricing.ModelPrice
			if err := rows.Scan(&pat, &p.InputPerMTok, &p.OutputPerMTok, &p.CacheReadPerMTok, &p.CacheWritePerMTok); err == nil {
				customPrices[pat] = p
			}
		}
	}
	return pricing.NewEngine(customPrices)
}

func (s *Store) calculateCacheSavings(whereClause string) (float64, error) {
	rClause := whereClause
	if rClause != "" {
		rClause = strings.Replace(rClause, "WHERE timestamp", "WHERE r.timestamp", 1)
	}
	query := fmt.Sprintf(`
		SELECT coalesce(r.model, ''), coalesce(sum(r.cache_read_tokens), 0)
		FROM requests r
		%s
		GROUP BY r.model
	`, rClause)

	rows, err := s.db.Query(query)
	if err != nil {
		return 0, err
	}
	defer rows.Close()

	engine := s.getPricingEngine()
	var totalSavings float64
	for rows.Next() {
		var model string
		var cacheTokens int64
		if err := rows.Scan(&model, &cacheTokens); err != nil {
			return 0, err
		}
		if cacheTokens <= 0 {
			continue
		}

		costIfInput, ok1 := engine.CalculateCost(model, cacheTokens, 0, 0, 0)
		costIfCache, ok2 := engine.CalculateCost(model, 0, 0, cacheTokens, 0)
		if ok1 && ok2 && costIfInput != nil && costIfCache != nil {
			savings := *costIfInput - *costIfCache
			if savings > 0 {
				totalSavings += savings
			}
		} else {
			// Fallback: (1.25 - 0.30) / 1,000,000.0
			totalSavings += float64(cacheTokens) * (1.25 - 0.30) / 1000000.0
		}
	}
	return totalSavings, rows.Err()
}


func (s *Store) GetCostOverTime(period string) ([]TimeSeriesPoint, error) {
	clause := periodClause(period)
	where := clause
	if where == "" {
		where = "WHERE 1=1"
	}

	query := fmt.Sprintf(`
		SELECT strftime('%%Y-%%m-%%d', timestamp) as day, tool, coalesce(sum(cost_usd), 0.0)
		FROM requests
		%s
		GROUP BY day, tool
		ORDER BY day ASC
	`, where)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]TimeSeriesPoint, 0)
	for rows.Next() {
		var pt TimeSeriesPoint
		if err := rows.Scan(&pt.Date, &pt.Tool, &pt.Cost); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetCostByModel(period string) ([]ModelCostPoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT coalesce(model, 'unknown'), coalesce(sum(cost_usd), 0.0), count(*)
		FROM requests
		%s
		GROUP BY model
		ORDER BY sum(cost_usd) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]ModelCostPoint, 0)
	for rows.Next() {
		var pt ModelCostPoint
		if err := rows.Scan(&pt.Model, &pt.Cost, &pt.RequestCount); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetTokensByModel(period string) ([]ModelTokenPoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT coalesce(model, 'unknown'), coalesce(sum(input_tokens), 0), coalesce(sum(output_tokens), 0)
		FROM requests
		%s
		GROUP BY model
		ORDER BY sum(total_tokens) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]ModelTokenPoint, 0)
	for rows.Next() {
		var pt ModelTokenPoint
		if err := rows.Scan(&pt.Model, &pt.InputTokens, &pt.OutputTokens); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetToolUsage(period string) ([]ToolUsagePoint, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT tool, count(*), coalesce(sum(cost_usd), 0.0)
		FROM requests
		%s
		GROUP BY tool
		ORDER BY count(*) DESC
	`, clause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	points := make([]ToolUsagePoint, 0)
	for rows.Next() {
		var pt ToolUsagePoint
		if err := rows.Scan(&pt.Tool, &pt.RequestCount, &pt.TotalCost); err == nil {
			points = append(points, pt)
		}
	}
	return points, nil
}

func (s *Store) GetRecentRequests(limit int, periodOpt ...string) ([]Request, error) {
	if limit <= 0 {
		limit = 50
	}
	period := ""
	if len(periodOpt) > 0 {
		period = periodOpt[0]
	}
	clause := periodClause(period)

	query := fmt.Sprintf(`
		SELECT id, tool, coalesce(session_id, ''), timestamp, coalesce(model, 'unknown'), input_tokens, output_tokens, total_tokens, cost_usd, latency_ms, coalesce(status, 'success')
		FROM requests
		%s
		ORDER BY timestamp DESC
		LIMIT ?
	`, clause)

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	reqs := make([]Request, 0)
	for rows.Next() {
		var r Request
		var tsStr string
		var cost sql.NullFloat64
		var latency sql.NullInt64

		if err := rows.Scan(&r.ID, &r.Tool, &r.SessionID, &tsStr, &r.Model, &r.InputTokens, &r.OutputTokens, &r.TotalTokens, &cost, &latency, &r.Status); err == nil {
			if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
				r.Timestamp = t
			} else if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
				r.Timestamp = t
			}
			if cost.Valid {
				r.CostUSD = &cost.Float64
			}
			if latency.Valid {
				r.LatencyMS = &latency.Int64
			}
			reqs = append(reqs, r)
		}
	}
	return reqs, nil
}

func (s *Store) GetCacheStats(period string) (*CacheStats, error) {
	clause := periodClause(period)
	query := fmt.Sprintf(`
		SELECT 
			coalesce(sum(cache_read_tokens), 0),
			coalesce(sum(input_tokens), 0),
			coalesce(sum(output_tokens), 0)
		FROM requests %s
	`, clause)

	var stats CacheStats
	err := s.db.QueryRow(query).Scan(&stats.CacheHitTokens, &stats.CacheMissTokens, &stats.OutputTokens)
	if err != nil {
		return nil, err
	}

	stats.TotalPrompt = stats.CacheHitTokens + stats.CacheMissTokens
	if stats.TotalPrompt > 0 {
		stats.CacheHitRate = (float64(stats.CacheHitTokens) / float64(stats.TotalPrompt)) * 100.0
	}

	savings, err := s.calculateCacheSavings(clause)
	if err == nil {
		stats.SavingsUSD = savings
	}

	return &stats, nil
}

func (s *Store) GetSessionSummaries(period string, limit int) ([]SessionSummary, error) {
	if limit <= 0 {
		limit = 20
	}
	clause := periodClause(period)
	where := clause
	if where == "" {
		where = "WHERE session_id IS NOT NULL AND session_id != ''"
	} else {
		where += " AND session_id IS NOT NULL AND session_id != ''"
	}

	query := fmt.Sprintf(`
		SELECT 
			session_id,
			tool,
			min(timestamp) as start_time,
			max(timestamp) as end_time,
			count(*) as turn_count,
			coalesce(sum(total_tokens), 0) as total_tokens,
			coalesce(sum(cache_read_tokens), 0) as cache_hit_tokens,
			coalesce(sum(input_tokens), 0) as cache_miss_tokens,
			coalesce(sum(cost_usd), 0.0) as total_cost,
			coalesce(sum(tool_call_count), 0) as tool_calls,
			coalesce(sum(tool_error_count), 0) as tool_errors,
			coalesce(sum(subagent_call_count), 0) as subagents
		FROM requests
		%s
		GROUP BY session_id, tool
		ORDER BY max(timestamp) DESC
		LIMIT ?
	`, where)

	rows, err := s.db.Query(query, limit)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	summaries := make([]SessionSummary, 0)
	for rows.Next() {
		var sm SessionSummary
		var startStr, endStr string
		err := rows.Scan(
			&sm.SessionID,
			&sm.Tool,
			&startStr,
			&endStr,
			&sm.TurnCount,
			&sm.TotalTokens,
			&sm.CacheHitTokens,
			&sm.CacheMissTokens,
			&sm.TotalCost,
			&sm.ToolCallCount,
			&sm.ToolErrorCount,
			&sm.SubagentCount,
		)
		if err != nil {
			continue
		}

		if t, err := time.Parse(time.RFC3339Nano, startStr); err == nil {
			sm.StartTime = t
		} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
			sm.StartTime = t
		}

		if t, err := time.Parse(time.RFC3339Nano, endStr); err == nil {
			sm.EndTime = t
		} else if t, err := time.Parse(time.RFC3339, endStr); err == nil {
			sm.EndTime = t
		}

		if !sm.EndTime.IsZero() && !sm.StartTime.IsZero() {
			sm.DurationSeconds = sm.EndTime.Sub(sm.StartTime).Seconds()
			if sm.DurationSeconds < 0 {
				sm.DurationSeconds = 0
			}
		}

		promptTotal := sm.CacheHitTokens + sm.CacheMissTokens
		if promptTotal > 0 {
			sm.CacheHitRate = (float64(sm.CacheHitTokens) / float64(promptTotal)) * 100.0
		}

		summaries = append(summaries, sm)
	}

	return summaries, nil
}

func (s *Store) GetSessionDetail(sessionID string) (*SessionDetail, error) {
	sessionID = strings.TrimSpace(sessionID)
	if sessionID == "" {
		return nil, sql.ErrNoRows
	}

	escapedID := strings.ReplaceAll(sessionID, "\\", "\\\\")
	escapedID = strings.ReplaceAll(escapedID, "%", "\\%")
	escapedID = strings.ReplaceAll(escapedID, "_", "\\_")

	var fullSessionID string
	if err := s.db.QueryRow(`
		SELECT session_id FROM requests
		WHERE session_id = ? OR session_id LIKE ? || '%' ESCAPE '\'
		LIMIT 1
	`, sessionID, escapedID).Scan(&fullSessionID); err != nil {
		return nil, err
	}
	sessionID = fullSessionID

	querySummary := `
		SELECT 
			session_id,
			tool,
			min(timestamp) as start_time,
			max(timestamp) as end_time,
			count(*) as turn_count,
			coalesce(sum(total_tokens), 0) as total_tokens,
			coalesce(sum(cache_read_tokens), 0) as cache_hit_tokens,
			coalesce(sum(input_tokens), 0) as cache_miss_tokens,
			coalesce(sum(cost_usd), 0.0) as total_cost,
			coalesce(sum(tool_call_count), 0) as tool_calls,
			coalesce(sum(tool_error_count), 0) as tool_errors,
			coalesce(sum(subagent_call_count), 0) as subagents
		FROM requests
		WHERE session_id = ?
		GROUP BY session_id, tool
	`

	var sm SessionSummary
	var startStr, endStr string
	err := s.db.QueryRow(querySummary, sessionID).Scan(

		&sm.SessionID,
		&sm.Tool,
		&startStr,
		&endStr,
		&sm.TurnCount,
		&sm.TotalTokens,
		&sm.CacheHitTokens,
		&sm.CacheMissTokens,
		&sm.TotalCost,
		&sm.ToolCallCount,
		&sm.ToolErrorCount,
		&sm.SubagentCount,
	)
	if err != nil {
		return nil, err
	}

	if t, err := time.Parse(time.RFC3339Nano, startStr); err == nil {
		sm.StartTime = t
	} else if t, err := time.Parse(time.RFC3339, startStr); err == nil {
		sm.StartTime = t
	}
	if t, err := time.Parse(time.RFC3339Nano, endStr); err == nil {
		sm.EndTime = t
	} else if t, err := time.Parse(time.RFC3339, endStr); err == nil {
		sm.EndTime = t
	}
	if !sm.EndTime.IsZero() && !sm.StartTime.IsZero() {
		sm.DurationSeconds = sm.EndTime.Sub(sm.StartTime).Seconds()
		if sm.DurationSeconds < 0 {
			sm.DurationSeconds = 0
		}
	}
	promptTotal := sm.CacheHitTokens + sm.CacheMissTokens
	if promptTotal > 0 {
		sm.CacheHitRate = (float64(sm.CacheHitTokens) / float64(promptTotal)) * 100.0
	}

	queryTurns := `
		SELECT 
			id, tool, coalesce(session_id, ''), timestamp, coalesce(model, 'unknown'),
			input_tokens, output_tokens, cache_read_tokens, cache_write_tokens, total_tokens,
			cost_usd, latency_ms, coalesce(status, 'success'),
			step_index, tool_call_count, tool_error_count, subagent_call_count
		FROM requests
		WHERE session_id = ?
		ORDER BY timestamp ASC, step_index ASC
	`
	rows, err := s.db.Query(queryTurns, sessionID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var turns []Request
	for rows.Next() {
		var r Request
		var tsStr string
		var cost sql.NullFloat64
		var latency sql.NullInt64

		err := rows.Scan(
			&r.ID, &r.Tool, &r.SessionID, &tsStr, &r.Model,
			&r.InputTokens, &r.OutputTokens, &r.CacheReadTokens, &r.CacheWriteTokens, &r.TotalTokens,
			&cost, &latency, &r.Status,
			&r.StepIndex, &r.ToolCallCount, &r.ToolErrorCount, &r.SubagentCallCount,
		)
		if err != nil {
			continue
		}

		if t, err := time.Parse(time.RFC3339Nano, tsStr); err == nil {
			r.Timestamp = t
		} else if t, err := time.Parse(time.RFC3339, tsStr); err == nil {
			r.Timestamp = t
		}
		if cost.Valid {
			r.CostUSD = &cost.Float64
		}
		if latency.Valid {
			r.LatencyMS = &latency.Int64
		}

		// Query tool calls for this turn
		tcRows, tcErr := s.db.Query(`
			SELECT id, request_id, session_id, tool_name, coalesce(status, 'success'), created_at
			FROM tool_calls
			WHERE request_id = ?
			ORDER BY created_at ASC
		`, r.ID)
		if tcErr == nil {
			for tcRows.Next() {
				var tc ToolCall
				var tcTimeStr string
				if err := tcRows.Scan(&tc.ID, &tc.RequestID, &tc.SessionID, &tc.ToolName, &tc.Status, &tcTimeStr); err == nil {
					if t, err := time.Parse(time.RFC3339Nano, tcTimeStr); err == nil {
						tc.CreatedAt = t
					} else if t, err := time.Parse(time.RFC3339, tcTimeStr); err == nil {
						tc.CreatedAt = t
					}
					r.ToolCalls = append(r.ToolCalls, tc)
				}
			}
			tcRows.Close()
		}

		turns = append(turns, r)
	}

	return &SessionDetail{
		Summary: sm,
		Turns:   turns,
	}, nil
}

func (s *Store) GetToolCallStats(period string) ([]ToolCallStat, error) {
	clause := periodClause(period)
	rClause := clause
	if rClause != "" {
		rClause = strings.Replace(rClause, "WHERE timestamp", "WHERE r.timestamp", 1)
	}

	query := fmt.Sprintf(`
		SELECT 
			tc.tool_name,
			count(*) as total_calls,
			coalesce(sum(case when tc.status = 'error' then 1 else 0 end), 0) as error_count
		FROM tool_calls tc
		JOIN requests r ON tc.request_id = r.id
		%s
		GROUP BY tc.tool_name
		ORDER BY total_calls DESC
	`, rClause)

	rows, err := s.db.Query(query)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	stats := make([]ToolCallStat, 0)
	for rows.Next() {
		var st ToolCallStat
		if err := rows.Scan(&st.ToolName, &st.TotalCalls, &st.ErrorCount); err == nil {
			stats = append(stats, st)
		}
	}
	return stats, nil
}

