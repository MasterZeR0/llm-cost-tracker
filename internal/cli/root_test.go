package cli_test

import (
	"bytes"
	"path/filepath"
	"strings"
	"testing"
	"time"

	"github.com/MasterZeR0/llm-cost-tracker/internal/cli"
	"github.com/MasterZeR0/llm-cost-tracker/internal/store"
)

func TestRootCmd_SetStoreForTestingPersists(t *testing.T) {
	tmpDir := t.TempDir()
	testDbPath := filepath.Join(tmpDir, "test_store_persists.db")
	testSt, err := store.NewStore(testDbPath)
	if err != nil {
		t.Fatal(err)
	}
	defer testSt.Close()

	// Seed test store with unique record
	cost := 0.42
	_, err = testSt.InsertRequests([]store.Request{
		{
			ID:          "unique-req-1",
			SessionID:   "unique-session-for-root-test",
			Tool:        "claude_code",
			Timestamp:   time.Now().UTC(),
			Model:       "claude-3-5-sonnet",
			StepIndex:   1,
			InputTokens: 1000,
			CostUSD:     &cost,
			Status:      "success",
		},
	})
	if err != nil {
		t.Fatal(err)
	}

	cli.SetStoreForTesting(testSt)
	defer cli.SetStoreForTesting(nil)

	var buf bytes.Buffer
	rootCmd := cli.GetRootCmdForTesting()
	rootCmd.SetOut(&buf)
	rootCmd.SetErr(&buf)
	rootCmd.SetArgs([]string{"summary", "--period=today"})

	if err := rootCmd.Execute(); err != nil {
		t.Fatalf("rootCmd.Execute() failed: %v", err)
	}

	out := buf.String()
	if !strings.Contains(out, "$0.42") {
		t.Fatalf("expected summary output to reflect test store cost $0.42, got: %s", out)
	}
}
