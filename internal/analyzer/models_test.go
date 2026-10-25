package analyzer_test

import (
	"encoding/json"
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/analyzer"
	"github.com/MasterZeR0/llm-cost-tracker/internal/config"
)

func TestDefaultConfig(t *testing.T) {
	cfg := analyzer.DefaultConfig()

	if !cfg.Enabled {
		t.Fatal("expected DefaultConfig to be enabled")
	}
	if cfg.SnowballTokenThreshold != 80000 {
		t.Fatalf("expected SnowballTokenThreshold 80000, got %d", cfg.SnowballTokenThreshold)
	}
	if cfg.SevereTurnTokenThreshold != 120000 {
		t.Fatalf("expected SevereTurnTokenThreshold 120000, got %d", cfg.SevereTurnTokenThreshold)
	}
	if cfg.WarnConsecutiveToolErrors != 3 {
		t.Fatalf("expected WarnConsecutiveToolErrors 3, got %d", cfg.WarnConsecutiveToolErrors)
	}
	if cfg.CacheChurnDropThreshold != 0.40 {
		t.Fatalf("expected CacheChurnDropThreshold 0.40, got %f", cfg.CacheChurnDropThreshold)
	}
}

func TestFromWasteMonitoringConfig(t *testing.T) {
	wm := config.WasteMonitoringConfig{
		Enabled:                   false,
		SnowballTokenThreshold:    60000,
		SevereTurnTokenThreshold:  90000,
		WarnConsecutiveToolErrors: 4,
		CacheChurnDropThreshold:   0.30,
		LogAlertsToConsole:        true,
	}

	cfg := analyzer.FromWasteMonitoringConfig(wm)

	if cfg.Enabled != wm.Enabled {
		t.Fatalf("expected Enabled %v, got %v", wm.Enabled, cfg.Enabled)
	}
	if cfg.SnowballTokenThreshold != wm.SnowballTokenThreshold {
		t.Fatalf("expected SnowballTokenThreshold %d, got %d", wm.SnowballTokenThreshold, cfg.SnowballTokenThreshold)
	}
	if cfg.SevereTurnTokenThreshold != wm.SevereTurnTokenThreshold {
		t.Fatalf("expected SevereTurnTokenThreshold %d, got %d", wm.SevereTurnTokenThreshold, cfg.SevereTurnTokenThreshold)
	}
	if cfg.WarnConsecutiveToolErrors != wm.WarnConsecutiveToolErrors {
		t.Fatalf("expected WarnConsecutiveToolErrors %d, got %d", wm.WarnConsecutiveToolErrors, cfg.WarnConsecutiveToolErrors)
	}
	if cfg.CacheChurnDropThreshold != wm.CacheChurnDropThreshold {
		t.Fatalf("expected CacheChurnDropThreshold %f, got %f", wm.CacheChurnDropThreshold, cfg.CacheChurnDropThreshold)
	}
}

func TestJSONSerialization(t *testing.T) {
	finding := analyzer.WasteFinding{
		Category:       analyzer.WasteContextSnowball,
		Severity:       analyzer.SeverityHigh,
		TurnIndex:      5,
		WastedTokens:   50000,
		WastedUSD:      0.75,
		Explanation:    "Context grew quadratically",
		Recommendation: "Run /compact or start a fresh session",
	}

	data, err := json.Marshal(finding)
	if err != nil {
		t.Fatalf("failed to marshal finding: %v", err)
	}

	var parsed analyzer.WasteFinding
	if err := json.Unmarshal(data, &parsed); err != nil {
		t.Fatalf("failed to unmarshal finding: %v", err)
	}

	if parsed.Category != analyzer.WasteContextSnowball {
		t.Fatalf("expected category %s, got %s", analyzer.WasteContextSnowball, parsed.Category)
	}
	if parsed.Severity != analyzer.SeverityHigh {
		t.Fatalf("expected severity %s, got %s", analyzer.SeverityHigh, parsed.Severity)
	}
	if parsed.TurnIndex != 5 {
		t.Fatalf("expected turn index 5, got %d", parsed.TurnIndex)
	}
	if parsed.WastedTokens != 50000 {
		t.Fatalf("expected wasted tokens 50000, got %d", parsed.WastedTokens)
	}
	if parsed.WastedUSD != 0.75 {
		t.Fatalf("expected wasted USD 0.75, got %f", parsed.WastedUSD)
	}
}
