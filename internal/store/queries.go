package store

import (
	"database/sql"
	"fmt"
	"time"
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
			coalesce(sum(total_tokens), 0), 
			coalesce(sum(cost_usd), 0.0), 
			coalesce(avg(latency_ms), 0.0)
		FROM requests %s
	`, clause)

	var stats OverviewStats
	err := s.db.QueryRow(query).Scan(&stats.TotalRequests, &stats.TotalTokens, &stats.TotalCost, &stats.AvgLatencyMS)
	return &stats, err
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

func (s *Store) GetRecentRequests(limit int) ([]Request, error) {
	if limit <= 0 {
		limit = 50
	}
	rows, err := s.db.Query(`
		SELECT id, tool, coalesce(session_id, ''), timestamp, coalesce(model, 'unknown'), input_tokens, output_tokens, total_tokens, cost_usd, latency_ms, coalesce(status, 'success')
		FROM requests
		ORDER BY timestamp DESC
		LIMIT ?
	`, limit)
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
			r.Timestamp, _ = time.Parse(time.RFC3339, tsStr)
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
