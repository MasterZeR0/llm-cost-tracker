package config

import (
	"os"
	"path/filepath"
	"testing"
)

func TestDefaultPathsForTool(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Fatalf("failed to get user home: %v", err)
	}

	tests := []struct {
		tool     string
		contains string
	}{
		{"cline", "saoudrizwan.claude-dev"},
		{"roo_code", "rooveterinaryinc.roo-cline"},
		{"cursor", "Cursor"},
		{"windsurf", "Windsurf"},
		{"copilot_cli", "copilot"},
	}

	for _, tc := range tests {
		paths := DefaultPathsForTool(tc.tool)
		if len(paths) == 0 {
			t.Errorf("expected paths for tool %s, got none", tc.tool)
			continue
		}
		found := false
		for _, p := range paths {
			if filepath.IsAbs(p) && len(p) > len(home) {
				found = true
				break
			}
		}
		if !found {
			t.Errorf("tool %s paths did not resolve to absolute home path: %v", tc.tool, paths)
		}
	}
}
