package config_test

import (
	"os"
	"path/filepath"
	"testing"

	"github.com/nehaverma/llm-cost-tracker/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := config.DefaultConfig()
	if cfg.DataDir == "" {
		t.Fatal("expected default DataDir to be non-empty")
	}
	if cfg.StoreContent {
		t.Fatal("expected default StoreContent to be false")
	}
	if !cfg.Tools["claude_code"].Enabled {
		t.Fatal("expected claude_code tool to be enabled by default")
	}
	if cfg.Dashboard.Port != 8384 {
		t.Fatalf("expected dashboard port 8384, got %d", cfg.Dashboard.Port)
	}
	if cfg.Daemon.PollInterval <= 0 {
		t.Fatalf("expected positive poll interval, got %v", cfg.Daemon.PollInterval)
	}
	if !cfg.Daemon.WatchFSEvents {
		t.Fatal("expected WatchFSEvents to be true by default")
	}
}

func TestLoadConfig_CustomFile(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
data_dir: /custom/data
store_content: true
log_level: debug
dashboard:
  port: 9000
  host: 0.0.0.0
tools:
  claude_code:
    enabled: false
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if cfg.DataDir != "/custom/data" {
		t.Fatalf("expected /custom/data, got %s", cfg.DataDir)
	}
	if !cfg.StoreContent {
		t.Fatal("expected StoreContent true")
	}
	if cfg.Dashboard.Port != 9000 {
		t.Fatalf("expected port 9000, got %d", cfg.Dashboard.Port)
	}
	if cfg.Tools["claude_code"].Enabled {
		t.Fatal("expected claude_code disabled")
	}
}

func TestLoadConfig_NonExistentFileReturnsDefault(t *testing.T) {
	tmpDir := t.TempDir()
	nonExistentPath := filepath.Join(tmpDir, "does-not-exist.yaml")

	cfg, err := config.LoadConfig(nonExistentPath)
	if err != nil {
		t.Fatalf("expected no error for nonexistent file, got: %v", err)
	}
	if cfg == nil {
		t.Fatal("expected non-nil default config")
	}
	if cfg.Dashboard.Port != 8384 {
		t.Fatalf("expected default port 8384, got %d", cfg.Dashboard.Port)
	}
}

func TestExpandHome(t *testing.T) {
	home, err := os.UserHomeDir()
	if err != nil {
		t.Skip("skipping home dir test: UserHomeDir failed")
	}

	expanded := config.ExpandHome("~/test/path")
	expected := filepath.Join(home, "test/path")
	if expanded != expected {
		t.Fatalf("expected %s, got %s", expected, expanded)
	}

	nonTilde := "/absolute/path"
	if config.ExpandHome(nonTilde) != nonTilde {
		t.Fatalf("expected %s, got %s", nonTilde, config.ExpandHome(nonTilde))
	}
}

func TestLoadConfig_DisableOtherTools(t *testing.T) {
	tmpDir := t.TempDir()
	cfgPath := filepath.Join(tmpDir, "config.yaml")

	yamlContent := `
disable_other_tools: true
tools:
  claude_code:
    enabled: true
`
	if err := os.WriteFile(cfgPath, []byte(yamlContent), 0644); err != nil {
		t.Fatal(err)
	}

	cfg, err := config.LoadConfig(cfgPath)
	if err != nil {
		t.Fatalf("failed to load config: %v", err)
	}
	if !cfg.Tools["claude_code"].Enabled {
		t.Fatal("expected claude_code enabled")
	}
	// All other default tools should be disabled
	if cfg.Tools["agy"].Enabled {
		t.Fatal("expected agy disabled by disable_other_tools")
	}
	if cfg.Tools["aider"].Enabled {
		t.Fatal("expected aider disabled by disable_other_tools")
	}
	if cfg.Tools["codex"].Enabled {
		t.Fatal("expected codex disabled by disable_other_tools")
	}
}
