package parsers_test

import (
	"sync"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
	"github.com/MasterZeR0/llm-cost-tracker/internal/parsers"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

type dummyParser struct {
	name string
}

func (d *dummyParser) Name() string { return d.name }
func (d *dummyParser) Discover(cfg config.ToolConfig) ([]parsers.DataSource, error) {
	return nil, nil
}
func (d *dummyParser) Parse(source parsers.DataSource, state store.SyncState) ([]store.Request, store.SyncState, error) {
	return nil, state, nil
}

func TestRegistry(t *testing.T) {
	// ClaudeCodeParser should already be registered via init()
	p, ok := parsers.Get("claude_code")
	if !ok || p == nil {
		t.Fatal("expected claude_code parser to be registered")
	}
	if p.Name() != "claude_code" {
		t.Errorf("expected claude_code name, got %s", p.Name())
	}

	p2, ok2 := parsers.GetParser("claude_code")
	if !ok2 || p2 == nil {
		t.Fatal("expected claude_code parser from GetParser")
	}

	_, ok = parsers.Get("non_existent_tool")
	if ok {
		t.Fatal("expected non_existent_tool to not be found")
	}

	all := parsers.All()
	if len(all) == 0 {
		t.Fatal("expected at least one parser in All()")
	}

	var foundClaudeCode bool
	for _, parser := range all {
		if parser.Name() == "claude_code" {
			foundClaudeCode = true
			break
		}
	}
	if !foundClaudeCode {
		t.Fatal("claude_code not found in All()")
	}
}

func TestRegistry_Concurrent(t *testing.T) {
	var wg sync.WaitGroup
	for i := 0; i < 20; i++ {
		wg.Add(2)
		go func(idx int) {
			defer wg.Done()
			parsers.Register(&dummyParser{name: "dummy"})
		}(i)
		go func(idx int) {
			defer wg.Done()
			_, _ = parsers.Get("claude_code")
			_ = parsers.All()
		}(i)
	}
	wg.Wait()
}
