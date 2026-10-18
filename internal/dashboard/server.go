package dashboard

import (
	"embed"
	"fmt"
	"io/fs"
	"net/http"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

//go:embed static/*
var staticFS embed.FS

type Server struct {
	cfg    *config.Config
	store  *store.Store
	router *http.ServeMux
}

func NewServer(cfg *config.Config, st *store.Store) *Server {
	s := &Server{
		cfg:    cfg,
		store:  st,
		router: http.NewServeMux(),
	}
	s.routes()
	return s
}

func (s *Server) Router() *http.ServeMux {
	return s.router
}

func (s *Server) routes() {
	s.router.HandleFunc("GET /api/overview", s.handleOverview)
	s.router.HandleFunc("GET /api/cost-over-time", s.handleCostOverTime)
	s.router.HandleFunc("GET /api/cost-by-model", s.handleCostByModel)
	s.router.HandleFunc("GET /api/tokens-by-model", s.handleTokensByModel)
	s.router.HandleFunc("GET /api/tool-usage", s.handleToolUsage)
	s.router.HandleFunc("GET /api/requests", s.handleRecentRequests)
	s.router.HandleFunc("GET /api/analytics/cache", s.handleAnalyticsCache)
	s.router.HandleFunc("GET /api/analytics/tools", s.handleAnalyticsTools)
	s.router.HandleFunc("GET /api/sessions", s.handleSessions)
	s.router.HandleFunc("GET /api/sessions/detail", s.handleSessionDetail)
	s.router.HandleFunc("GET /api/sessions/{id}", s.handleSessionDetail)

	sub, err := fs.Sub(staticFS, "static")
	if err == nil {
		s.router.Handle("/", http.FileServer(http.FS(sub)))
	}
}

func (s *Server) Start() error {
	addr := fmt.Sprintf("%s:%d", s.cfg.Dashboard.Host, s.cfg.Dashboard.Port)
	return http.ListenAndServe(addr, s.router)
}
