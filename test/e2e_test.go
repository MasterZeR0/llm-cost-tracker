package test

import (
	"bytes"
	"path/filepath"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/cli"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/daemon"
	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
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
	if overview.TotalSessions == 0 {
		t.Fatalf("expected positive sessions in overview: %+v", overview)
	}

	cacheStats, err := st.GetCacheStats("all")
	if err != nil {
		t.Fatalf("cache stats error: %v", err)
	}
	if cacheStats.TotalPrompt == 0 {
		t.Fatalf("expected positive prompt tokens: %+v", cacheStats)
	}

	sessions, err := st.GetSessionSummaries("all", 10)
	if err != nil {
		t.Fatalf("session summaries error: %v", err)
	}
	if len(sessions) == 0 {
		t.Fatalf("expected at least 1 session summary, got %d", len(sessions))
	}

	var sumBuf bytes.Buffer
	if err := cli.RenderSummary(&sumBuf, st, "all"); err != nil {
		t.Fatalf("RenderSummary error: %v", err)
	}
	sumOut := sumBuf.String()
	if !bytes.Contains([]byte(sumOut), []byte("Total Sessions:")) {
		t.Errorf("expected Total Sessions in summary output: %s", sumOut)
	}
	if !bytes.Contains([]byte(sumOut), []byte("Cache Hit Rate:")) {
		t.Errorf("expected Cache Hit Rate in summary output: %s", sumOut)
	}

	var sessBuf bytes.Buffer
	if err := cli.RenderSessionsList(&sessBuf, st, "all", 10); err != nil {
		t.Fatalf("RenderSessionsList error: %v", err)
	}
	sessOut := sessBuf.String()
	if !bytes.Contains([]byte(sessOut), []byte("Recent Sessions")) {
		t.Errorf("expected Recent Sessions in sessions output: %s", sessOut)
	}
}

