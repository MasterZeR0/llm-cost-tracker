package parsers

import (
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type DataSource struct {
	Path string
	Type string // "jsonl" or "sqlite"
}

type Parser interface {
	Name() string
	Discover(cfg config.ToolConfig) ([]DataSource, error)
	Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error)
}
