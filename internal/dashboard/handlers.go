package dashboard

import (
	"encoding/json"
	"net/http"
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

func (s *Server) handleRecentRequests(w http.ResponseWriter, r *http.Request) {
	period := r.URL.Query().Get("period")
	reqs, err := s.store.GetRecentRequests(50, period)
	if err != nil {
		http.Error(w, err.Error(), http.StatusInternalServerError)
		return
	}
	writeJSON(w, http.StatusOK, reqs)
}
