package config

import (
	"os"
	"path/filepath"
	"strings"
	"time"

	"gopkg.in/yaml.v3"
)

type ToolConfig struct {
	Enabled      bool              `yaml:"enabled"`
	Paths        []string          `yaml:"paths"`
	AnalyticsLog string            `yaml:"analytics_log,omitempty"`
	Extra        map[string]string `yaml:"extra,omitempty"`
}

type DaemonConfig struct {
	PollInterval  time.Duration `yaml:"poll_interval"`
	WatchFSEvents bool          `yaml:"watch_fs_events"`
}

type DashboardConfig struct {
	Port int    `yaml:"port"`
	Host string `yaml:"host"`
}

type WasteMonitoringConfig struct {
	Enabled                   bool    `yaml:"enabled"`
	SnowballTokenThreshold    int64   `yaml:"snowball_token_threshold"`
	SevereTurnTokenThreshold  int64   `yaml:"severe_turn_token_threshold"`
	WarnConsecutiveToolErrors int     `yaml:"warn_consecutive_tool_errors"`
	CacheChurnDropThreshold   float64 `yaml:"cache_churn_drop_threshold"`
	LogAlertsToConsole        bool    `yaml:"log_alerts_to_console"`
}

type Config struct {
	DataDir           string                `yaml:"data_dir"`
	StoreContent      bool                  `yaml:"store_content"`
	LogLevel          string                `yaml:"log_level"`
	DisableOtherTools bool                  `yaml:"disable_other_tools,omitempty"`
	Tools             map[string]ToolConfig `yaml:"tools"`
	Daemon            DaemonConfig          `yaml:"daemon"`
	Dashboard         DashboardConfig       `yaml:"dashboard"`
	WasteMonitoring   WasteMonitoringConfig `yaml:"waste_monitoring"`
}

func ExpandHome(path string) string {
	if path == "~" {
		home, err := os.UserHomeDir()
		if err == nil {
			return home
		}
	} else if strings.HasPrefix(path, "~/") {
		home, err := os.UserHomeDir()
		if err == nil {
			return filepath.Join(home, path[2:])
		}
	}
	return path
}

func DefaultConfig() *Config {
	home, _ := os.UserHomeDir()
	dataDir := filepath.Join(home, ".local", "share", "llm-cost-tracker")

	return &Config{
		DataDir:      dataDir,
		StoreContent: false,
		LogLevel:     "info",
		Tools: map[string]ToolConfig{
			"claude_code": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".claude", "projects")},
			},
			"aider": {
				Enabled:      true,
				AnalyticsLog: "",
			},
			"codex": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".codex", "state_5.sqlite")},
			},
			"agy": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".gemini", "antigravity-cli", "brain")},
			},
			"opencode": {
				Enabled: true,
				Paths:   []string{filepath.Join(home, ".local", "share", "opencode", "opencode.db")},
			},
			"cline": {
				Enabled: true,
				Paths:   DefaultPathsForTool("cline"),
			},
			"roo_code": {
				Enabled: true,
				Paths:   DefaultPathsForTool("roo_code"),
			},
			"cursor": {
				Enabled: true,
				Paths:   DefaultPathsForTool("cursor"),
			},
			"windsurf": {
				Enabled: true,
				Paths:   DefaultPathsForTool("windsurf"),
			},
			"copilot_cli": {
				Enabled: true,
				Paths:   DefaultPathsForTool("copilot_cli"),
			},
		},
		Daemon: DaemonConfig{
			PollInterval:  30 * time.Second,
			WatchFSEvents: true,
		},
		Dashboard: DashboardConfig{
			Port: 8384,
			Host: "127.0.0.1",
		},
		WasteMonitoring: WasteMonitoringConfig{
			Enabled:                   true,
			SnowballTokenThreshold:    80000,
			SevereTurnTokenThreshold:  120000,
			WarnConsecutiveToolErrors: 3,
			CacheChurnDropThreshold:   0.40,
			LogAlertsToConsole:        true,
		},
	}
}

func LoadConfig(path string) (*Config, error) {
	cfg := DefaultConfig()
	expandedPath := ExpandHome(path)
	data, err := os.ReadFile(expandedPath)
	if err != nil {
		if os.IsNotExist(err) {
			return cfg, nil
		}
		return nil, err
	}

	var raw struct {
		DisableOtherTools bool                  `yaml:"disable_other_tools"`
		Tools             map[string]ToolConfig `yaml:"tools"`
	}
	_ = yaml.Unmarshal(data, &raw)

	if err := yaml.Unmarshal(data, cfg); err != nil {
		return nil, err
	}
	if raw.DisableOtherTools {
		for toolName, toolCfg := range cfg.Tools {
			if _, specified := raw.Tools[toolName]; !specified {
				toolCfg.Enabled = false
				cfg.Tools[toolName] = toolCfg
			}
		}
	}
	cfg.DataDir = ExpandHome(cfg.DataDir)
	return cfg, nil
}
