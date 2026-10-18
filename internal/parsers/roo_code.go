package parsers

import (
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type RooCodeParser struct{}

func init() {
	Register(&RooCodeParser{})
}

func (p *RooCodeParser) Name() string {
	return "roo_code"
}

func (p *RooCodeParser) Discover(cfg config.ToolConfig) ([]DataSource, error) {
	return discoverTaskConversationFiles(cfg)
}

func (p *RooCodeParser) Parse(source DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	return parseTaskConversationHistory(source, state, "roo_code")
}
