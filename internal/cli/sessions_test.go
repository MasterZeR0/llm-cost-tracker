package cli_test

import (
	"bytes"
	"path/filepath"
	"testing"
	"time"

	"github.com/nehaverma/llm-cost-tracker/internal/cli"
	"github.com/nehaverma/llm-cost-tracker/internal/store"
)

func TestRenderSessionsList_And_Detail(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "sessions_test.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	sessID := "test-session-xyz"
	cost := 0.05
	req := store.Request{
		ID:                "req-1",
		Tool:              "agy",
		SessionID:         sessID,
		Timestamp:         time.Now().UTC(),
		Model:             "gemini-2-pro",
		InputTokens:       500,
		OutputTokens:      100,
		CacheReadTokens:   1500,
		TotalTokens:       2100,
		CostUSD:           &cost,
		StepIndex:         0,
		ToolCallCount:     1,
		SubagentCallCount: 0,
		ToolCalls: []store.ToolCall{
			{
				ID:        "tc-1",
				RequestID: "req-1",
				SessionID: sessID,
				ToolName:  "view_file",
				Status:    "success",
				CreatedAt: time.Now().UTC(),
			},
		},
	}
	if err := st.InsertRequestsWithTools([]store.Request{req}); err != nil {
		t.Fatal(err)
	}

	// 1. Test RenderSessionsList
	var listBuf bytes.Buffer
	if err := cli.RenderSessionsList(&listBuf, st, "today", 10); err != nil {
		t.Fatalf("unexpected error rendering sessions list: %v", err)
	}
	listOut := listBuf.String()
	if !bytes.Contains([]byte(listOut), []byte("Recent Sessions")) {
		t.Errorf("expected 'Recent Sessions' header in output: %s", listOut)
	}
	if !bytes.Contains([]byte(listOut), []byte("test-session-xyz")) {
		t.Errorf("expected session ID in output: %s", listOut)
	}
	if !bytes.Contains([]byte(listOut), []byte("75.0%")) { // 1500 hit / 2000 prompt
		t.Errorf("expected 75.0%% cache hit rate in output: %s", listOut)
	}

	// 2. Test RenderSessionDetail
	var detailBuf bytes.Buffer
	if err := cli.RenderSessionDetail(&detailBuf, st, sessID); err != nil {
		t.Fatalf("unexpected error rendering session detail: %v", err)
	}
	detailOut := detailBuf.String()
	if !bytes.Contains([]byte(detailOut), []byte("Session Detail: test-session-xyz")) {
		t.Errorf("expected header with session ID: %s", detailOut)
	}
	if !bytes.Contains([]byte(detailOut), []byte("view_file")) {
		t.Errorf("expected tool name 'view_file' in turn table: %s", detailOut)
	}

	// 3. Test non-existent session
	var missingBuf bytes.Buffer
	if err := cli.RenderSessionDetail(&missingBuf, st, "non-existent-session"); err == nil {
		t.Error("expected error for non-existent session, got nil")
	}
}

func TestRenderSessionsList_Empty(t *testing.T) {
	tmpDir := t.TempDir()
	st, err := store.NewStore(filepath.Join(tmpDir, "empty_sessions.db"))
	if err != nil {
		t.Fatal(err)
	}
	defer st.Close()

	var buf bytes.Buffer
	if err := cli.RenderSessionsList(&buf, st, "all", 10); err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	out := buf.String()
	if !bytes.Contains([]byte(out), []byte("(no sessions recorded for this period)")) {
		t.Errorf("expected empty notice: %s", out)
	}
}
