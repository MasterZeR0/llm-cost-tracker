package test

import (
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
	"github.com/nehaverma/llm-cost-tracker/internal/daemon"
	"github.com/nehaverma/llm-cost-tracker/internal/pricing"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestEndToEndPipeline(t *testing.T) {
	tmpDir := t.TempDir()
	dbPath := filepath.Join(tmpDir, "e2e.db")

	st, err := store.NewStore(dbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	engine := pricing.NewEngine(nil)

	fixtureDir, _ := filepath.Abs("../internal/parsers/testdata")
	cfg := config.DefaultConfig()
	cfg.Tools["claude_code"] = config.ToolConfig{Enabled: true, Paths: []string{fixtureDir}}
	cfg.Tools["agy"] = config.ToolConfig{Enabled: true, Paths: []string{fixtureDir}}
	cfg.Tools["aider"] = config.ToolConfig{Enabled: false}
	cfg.Tools["codex"] = config.ToolConfig{Enabled: false}
	cfg.Tools["opencode"] = config.ToolConfig{Enabled: false}

	imported, err := daemon.SyncAll(cfg, st, engine)
	if err != nil {
		t.Fatalf("sync error: %v", err)
	}
	if imported < 2 {
		t.Fatalf("expected at least 2 records imported, got %d", imported)
	}

	overview, err := st.GetOverview("all")
	if err != nil {
		t.Fatal(err)
	}
	if overview.TotalRequests == 0 || overview.TotalTokens == 0 {
		t.Fatalf("expected positive metrics in overview: %+v", overview)
	}
}
