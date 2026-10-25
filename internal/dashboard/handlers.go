package dashboard

import (
	"database/sql"
	"encoding/json"
	"errors"
	"net/http"
	"strconv"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
)

func writeJSON(w http.ResponseWriter, status int, data any) {
	w.Header().Set("Content-Type", "application/json")
	w.WriteHeader(status)
	_ = json.NewEncoder(w).Encode(data)
}

func (s *Server) handleOverview(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	stats, err := s.store.GetOverview(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleCostOverTime(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetCostOverTime(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleCostByModel(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetCostByModel(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleTokensByModel(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetTokensByModel(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleToolUsage(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetToolUsage(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleLatencyOverTime(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetLatencyOverTime(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleModelUsage(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	points, err := s.store.GetModelUsage(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, points)
}

func (s *Server) handleRecentRequests(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	reqs, err := s.store.GetRecentRequests(50, period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}

func (s *Server) handleAnalyticsCache(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	stats, err := s.store.GetCacheStats(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleAnalyticsTools(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	stats, err := s.store.GetToolCallStats(period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, stats)
}

func (s *Server) handleSessions(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	limit := 50
	if l := r.URL.Query().Get("limit"); l != "" {
		if val, err := strconv.Atoi(l); err == nil && val > 0 {
			limit = val
		}
	}
	sessions, err := s.store.GetSessionSummaries(period, limit)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, sessions)
}

func (s *Server) handleSessionDetail(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		id = r.PathValue("id")
	}
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}
	detail, err := s.store.GetSessionDetail(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}
	writeJSON(w, http.StatusOK, detail)
}

func (s *Server) getAnalyzerConfig() analyzer.Config {
	if s.cfg == nil {
		return analyzer.DefaultConfig()
	}
	cfg := analyzer.FromWasteMonitoringConfig(s.cfg.WasteMonitoring)
	if cfg.SnowballTokenThreshold == 0 {
		return analyzer.DefaultConfig()
	}
	return cfg
}

func (s *Server) handleWaste(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	switch period {
	case "today", "week", "month", "all":
		// valid
	default:
		period = "today"
	}

	details, err := s.store.GetPeriodSessionDetails(period)
	if err != nil {
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	cfg := s.getAnalyzerConfig()
	summary := analyzer.AnalyzePeriod(details, period, cfg)
	writeJSON(w, http.StatusOK, summary)
}

func (s *Server) handleSessionWaste(w http.ResponseWriter, r *http.Request) {
	id := r.URL.Query().Get("id")
	if id == "" {
		id = r.PathValue("id")
	}
	if id == "" {
		http.Error(w, "missing session id", http.StatusBadRequest)
		return
	}

	detail, err := s.store.GetSessionDetail(id)
	if err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			writeJSON(w, http.StatusNotFound, map[string]string{"error": "session not found"})
			return
		}
		writeJSON(w, http.StatusInternalServerError, map[string]string{"error": err.Error()})
		return
	}

	cfg := s.getAnalyzerConfig()
	analysis := analyzer.AnalyzeSession(*detail, cfg)
	writeJSON(w, http.StatusOK, analysis)
}
