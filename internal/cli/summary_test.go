package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/cli"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestRenderSummary(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	cost := 1.25
	lat := int64(1500)
	_, _ = st.InsertRequests([]store.Request{
		{
			ID: "1", Tool: "claude_code", Timestamp: time.Now().UTC(),
			Model: "claude-3-7-sonnet", InputTokens: 5000, OutputTokens: 1000, TotalTokens: 6000,
			CostUSD: &cost, LatencyMS: &lat, Status: "success",
		},
	})

	var buf bytes.Buffer
	err = cli.RenderSummary(&buf, st, "today")
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("LLM Cost Tracker")) {
		t.Fatalf("expected title in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("Total Cost:        $1.25")) {
		t.Fatalf("expected total cost $1.25 in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("Total Sessions:")) {
		t.Fatalf("expected Total Sessions in output: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("Cache Hit Rate:")) {
		t.Fatalf("expected Cache Hit Rate in output: %s", out)
	}
}

func TestRenderSummary_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "empty.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var buf bytes.Buffer
	err = cli.RenderSummary(&buf, st, "all")
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("Total Cost:        $0.00")) {
		t.Fatalf("expected total cost $0.00: %s", out)
	}
	if bytes.Contains([]byte(out), []byte("By Tool:")) {
		t.Fatalf("did not expect By Tool in empty summary: %s", out)
	}
}


func TestRenderSummary_WithBreakdown(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "breakdown.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	c1 := 0.50
	c2 := 1.50
	l1 := int64(800)
	l2 := int64(1200)

	_, _ = st.InsertRequests([]store.Request{
		{
			ID: "1", Tool: "claude_code", Timestamp: time.Now().UTC(),
			Model: "claude-3-7-sonnet", InputTokens: 1000, OutputTokens: 500, TotalTokens: 1500,
			CostUSD: &c1, LatencyMS: &l1, Status: "success",
		},
		{
			ID: "2", Tool: "aider", Timestamp: time.Now().UTC(),
			Model: "gpt-4o", InputTokens: 2000, OutputTokens: 800, TotalTokens: 2800,
			CostUSD: &c2, LatencyMS: &l2, Status: "success",
		},
	})

	var buf bytes.Buffer
	err = cli.RenderSummary(&buf, st, "today")
	if err != nil {
		t.Fatal(err)
	}

	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("By Tool:")) {
		t.Fatalf("expected By Tool section: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("claude_code")) || !bytes.Contains([]byte(out), []byte("aider")) {
		t.Fatalf("expected tool names: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("By Model:")) {
		t.Fatalf("expected By Model section: %s", out)
	}
	if !bytes.Contains([]byte(out), []byte("claude-3-7-sonnet")) || !bytes.Contains([]byte(out), []byte("gpt-4o")) {
		t.Fatalf("expected model names: %s", out)
	}
}

func TestRenderSummary_InvalidPeriod(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "invalid.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var buf bytes.Buffer
	err = cli.RenderSummary(&buf, st, "invalid_period")
	if err == nil {
		t.Fatal("expected error for invalid period, got nil")
	}
}
