package pricing_test

import (
	"testing"

	"github.com/MasterZeR0/llm-cost-tracker/internal/pricing"
)

func TestPricingEngine_StandardModels(t *testing.T) {
	e := pricing.NewEngine(nil)

	// Claude Opus base: Input $15/m, Output $75/m
	cost, ok := e.CalculateCost("claude-opus-4-20250514", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected model to match")
	}
	// 15 + (0.1 * 75) = 15 + 7.5 = 22.5
	expected := 22.50
	if cost == nil || *cost < expected-0.01 || *cost > expected+0.01 {
		t.Fatalf("expected cost ~22.50, got %v", cost)
	}

	// Claude Opus 5: Input $4/m, Output $20/m
	costOpus5, ok := e.CalculateCost("claude-opus-5", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected claude-opus-5 to match")
	}
	// 4 + (0.1 * 20) = 4 + 2 = 6.00
	if costOpus5 == nil || *costOpus5 < 5.99 || *costOpus5 > 6.01 {
		t.Fatalf("expected cost ~6.00 for claude-opus-5, got %v", costOpus5)
	}

	// OpenAI o1: Input $15/m, Output $60/m
	costO1, ok := e.CalculateCost("o1", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected o1 to match")
	}
	// 15 + (0.1 * 60) = 21.00
	if costO1 == nil || *costO1 < 20.99 || *costO1 > 21.01 {
		t.Fatalf("expected cost ~21.00 for o1, got %v", costO1)
	}

	// OpenAI o3-mini: Input $1.10/m, Output $4.40/m
	costO3Mini, ok := e.CalculateCost("o3-mini", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected o3-mini to match")
	}
	// 1.10 + 0.44 = 1.54
	if costO3Mini == nil || *costO3Mini < 1.53 || *costO3Mini > 1.55 {
		t.Fatalf("expected cost ~1.54 for o3-mini, got %v", costO3Mini)
	}

	// Gemini 3.8 Flash: Input $0.10/m, Output $0.40/m
	costGemini3Flash, ok := e.CalculateCost("gemini-3-8-flash", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected gemini-3-8-flash to match")
	}
	// 0.10 + 0.04 = 0.14
	if costGemini3Flash == nil || *costGemini3Flash < 0.139 || *costGemini3Flash > 0.141 {
		t.Fatalf("expected cost ~0.14 for gemini-3-8-flash, got %v", costGemini3Flash)
	}

	// Claude Opus 4-6 Thinking: Input $5/m, Output $25/m
	costOpus46, ok := e.CalculateCost("claude-opus-4-6-thinking", 1_000_000, 100_000, 0, 0)
	if !ok {
		t.Fatal("expected claude-opus-4-6-thinking to match")
	}
	// 5.00 + 2.50 = 7.50
	if costOpus46 == nil || *costOpus46 < 7.49 || *costOpus46 > 7.51 {
		t.Fatalf("expected cost ~7.50 for claude-opus-4-6-thinking, got %v", costOpus46)
	}

	// Unknown model
	unknownCost, ok := e.CalculateCost("totally-unknown-model-xyz", 1000, 1000, 0, 0)
	if ok || unknownCost != nil {
		t.Fatalf("expected unknown model to return false and nil cost, got %v, %v", ok, unknownCost)
	}
}

func TestPricingEngine_CacheTokens(t *testing.T) {
	e := pricing.NewEngine(nil)

	// Test cache read discount
	cost, ok := e.CalculateCost("claude-3-5-sonnet", 100_000, 10_000, 500_000, 0)
	if !ok || cost == nil {
		t.Fatal("expected valid calculation")
	}
	if *cost <= 0 {
		t.Fatal("cost should be positive")
	}
}

func TestPricingEngine_CustomPrices(t *testing.T) {
	custom := map[string]pricing.ModelPrice{
		"custom-llama-3": {
			InputPerMTok:  0.50,
			OutputPerMTok: 1.50,
		},
		"gpt-4o": {
			InputPerMTok:  2.00, // overridden from 2.50
			OutputPerMTok: 8.00, // overridden from 10.00
		},
	}
	e := pricing.NewEngine(custom)

	// Test custom model
	cost, ok := e.CalculateCost("custom-llama-3", 1_000_000, 1_000_000, 0, 0)
	if !ok || cost == nil {
		t.Fatal("expected custom model to match")
	}
	expected := 2.00
	if *cost < expected-0.01 || *cost > expected+0.01 {
		t.Fatalf("expected cost ~2.00, got %f", *cost)
	}

	// Test overridden model
	costOverride, ok := e.CalculateCost("gpt-4o", 1_000_000, 1_000_000, 0, 0)
	if !ok || costOverride == nil {
		t.Fatal("expected overridden gpt-4o to match")
	}
	expectedOverride := 10.00 // 2.00 + 8.00
	if *costOverride < expectedOverride-0.01 || *costOverride > expectedOverride+0.01 {
		t.Fatalf("expected cost ~10.00, got %f", *costOverride)
	}
}

func TestPricingEngine_EmptyModel(t *testing.T) {
	e := pricing.NewEngine(nil)
	cost, ok := e.CalculateCost("", 100, 100, 0, 0)
	if ok || cost != nil {
		t.Fatalf("expected empty model to return false and nil, got ok=%v, cost=%v", ok, cost)
	}
}

func TestPricingEngine_GeminiWildcards(t *testing.T) {
	e := pricing.NewEngine(nil)

	// "gemini-2*flash*"
	cost, ok := e.CalculateCost("gemini-2.0-flash-exp", 1_000_000, 1_000_000, 0, 0)
	if !ok || cost == nil {
		t.Fatal("expected gemini-2.0-flash-exp to match gemini-2*flash*")
	}
	// Input: 0.10, Output: 0.40 -> total 0.50
	if *cost < 0.49 || *cost > 0.51 {
		t.Fatalf("expected cost ~0.50, got %f", *cost)
	}
}

func TestPricingEngine_ModelSpecificity(t *testing.T) {
	e := pricing.NewEngine(nil)

	// gpt-4o-mini should match gpt-4o-mini ($0.15/$0.60), not gpt-4o ($2.50/$10.00)
	costMini, ok := e.CalculateCost("gpt-4o-mini", 1_000_000, 1_000_000, 0, 0)
	if !ok || costMini == nil {
		t.Fatal("expected gpt-4o-mini to match")
	}
	expectedMini := 0.75 // 0.15 + 0.60
	if *costMini < expectedMini-0.001 || *costMini > expectedMini+0.001 {
		t.Fatalf("expected gpt-4o-mini cost ~0.75, got %f", *costMini)
	}

	// gpt-4o-mini-2024-07-18 substring match should also match gpt-4o-mini
	costMiniDate, ok := e.CalculateCost("gpt-4o-mini-2024-07-18", 1_000_000, 1_000_000, 0, 0)
	if !ok || costMiniDate == nil {
		t.Fatal("expected gpt-4o-mini-2024-07-18 to match")
	}
	if *costMiniDate < expectedMini-0.001 || *costMiniDate > expectedMini+0.001 {
		t.Fatalf("expected gpt-4o-mini-2024-07-18 cost ~0.75, got %f", *costMiniDate)
	}

	// gpt-4o-2024-08-06 should match gpt-4o ($2.50/$10.00 = $12.50)
	cost4oDate, ok := e.CalculateCost("gpt-4o-2024-08-06", 1_000_000, 1_000_000, 0, 0)
	if !ok || cost4oDate == nil {
		t.Fatal("expected gpt-4o-2024-08-06 to match")
	}
	expected4o := 12.50
	if *cost4oDate < expected4o-0.01 || *cost4oDate > expected4o+0.01 {
		t.Fatalf("expected gpt-4o-2024-08-06 cost ~12.50, got %f", *cost4oDate)
	}
}

func TestPricingEngine_CaseInsensitive(t *testing.T) {
	e := pricing.NewEngine(nil)

	cost, ok := e.CalculateCost("GPT-4O-MINI", 1_000_000, 1_000_000, 0, 0)
	if !ok || cost == nil {
		t.Fatal("expected case-insensitive match")
	}
	expected := 0.75
	if *cost < expected-0.001 || *cost > expected+0.001 {
		t.Fatalf("expected cost ~0.75, got %f", *cost)
	}
}

func TestPricingEngine_ProviderPrefixedModels(t *testing.T) {
	e := pricing.NewEngine(nil)

	// anthropic/claude-3-5-sonnet-20241022 with wildcard claude-3-5-sonnet*
	costSonnet, ok := e.CalculateCost("anthropic/claude-3-5-sonnet-20241022", 1_000_000, 1_000_000, 0, 0)
	if !ok || costSonnet == nil {
		t.Fatal("expected anthropic/claude-3-5-sonnet-20241022 to match claude-3-5-sonnet*")
	}
	expectedSonnet := 18.00 // 3.00 + 15.00
	if *costSonnet < expectedSonnet-0.01 || *costSonnet > expectedSonnet+0.01 {
		t.Fatalf("expected cost ~18.00, got %f", *costSonnet)
	}

	// openai/gpt-4o
	cost4o, ok := e.CalculateCost("openai/gpt-4o", 1_000_000, 1_000_000, 0, 0)
	if !ok || cost4o == nil {
		t.Fatal("expected openai/gpt-4o to match gpt-4o")
	}
	expected4o := 12.50 // 2.50 + 10.00
	if *cost4o < expected4o-0.01 || *cost4o > expected4o+0.01 {
		t.Fatalf("expected cost ~12.50, got %f", *cost4o)
	}

	// openai/gpt-4o-mini
	costMini, ok := e.CalculateCost("openai/gpt-4o-mini", 1_000_000, 1_000_000, 0, 0)
	if !ok || costMini == nil {
		t.Fatal("expected openai/gpt-4o-mini to match gpt-4o-mini")
	}
	expectedMini := 0.75 // 0.15 + 0.60
	if *costMini < expectedMini-0.001 || *costMini > expectedMini+0.001 {
		t.Fatalf("expected cost ~0.75, got %f", *costMini)
	}

	// google/gemini-2.0-flash-exp
	costFlash, ok := e.CalculateCost("google/gemini-2.0-flash-exp", 1_000_000, 1_000_000, 0, 0)
	if !ok || costFlash == nil {
		t.Fatal("expected google/gemini-2.0-flash-exp to match gemini-2*flash*")
	}
	expectedFlash := 0.50 // 0.10 + 0.40
	if *costFlash < expectedFlash-0.01 || *costFlash > expectedFlash+0.01 {
		t.Fatalf("expected cost ~0.50, got %f", *costFlash)
	}

	// nested / multi-prefix: openrouter/anthropic/claude-3-5-sonnet
	costOpenRouter, ok := e.CalculateCost("openrouter/anthropic/claude-3-5-sonnet", 1_000_000, 1_000_000, 0, 0)
	if !ok || costOpenRouter == nil {
		t.Fatal("expected openrouter/anthropic/claude-3-5-sonnet to match")
	}
	if *costOpenRouter < expectedSonnet-0.01 || *costOpenRouter > expectedSonnet+0.01 {
		t.Fatalf("expected cost ~18.00, got %f", *costOpenRouter)
	}

	// Custom pattern with provider prefix matching unprefixed input
	custom := map[string]pricing.ModelPrice{
		"mistralai/mistral-large": {
			InputPerMTok:  2.00,
			OutputPerMTok: 6.00,
		},
	}
	eCustom := pricing.NewEngine(custom)
	costMistral, ok := eCustom.CalculateCost("mistral-large", 1_000_000, 1_000_000, 0, 0)
	if !ok || costMistral == nil {
		t.Fatal("expected mistral-large to match custom mistralai/mistral-large")
	}
	expectedMistral := 8.00
	if *costMistral < expectedMistral-0.01 || *costMistral > expectedMistral+0.01 {
		t.Fatalf("expected cost ~8.00, got %f", *costMistral)
	}
}

func TestPricingEngine_NewAssistantModels(t *testing.T) {
	e := pricing.NewEngine(nil)

	tests := []struct {
		model            string
		inputTokens      int64
		outputTokens     int64
		cacheReadTokens  int64
		cacheWriteTokens int64
		expectedCost     float64
	}{
		// Claude 3 Opus exact/dated
		{
			model:        "claude-3-opus-20240229",
			inputTokens:  1_000_000,
			outputTokens: 100_000,
			expectedCost: 15.00 + 7.50, // 22.50
		},
		// Claude 3 Haiku exact/dated
		{
			model:        "claude-3-haiku-20240307",
			inputTokens:  1_000_000,
			outputTokens: 100_000,
			expectedCost: 0.80 + 0.40, // 1.20
		},
		// GPT-4.5
		{
			model:           "gpt-4.5-preview",
			inputTokens:     1_000_000,
			outputTokens:    100_000,
			cacheReadTokens: 1_000_000,
			expectedCost:    75.00 + 15.00 + 37.50, // 127.50
		},
		// GPT-4-turbo
		{
			model:        "gpt-4-turbo-2024-04-09",
			inputTokens:  1_000_000,
			outputTokens: 100_000,
			expectedCost: 10.00 + 3.00, // 13.00
		},
		// Legacy base GPT-4
		{
			model:        "gpt-4",
			inputTokens:  1_000_000,
			outputTokens: 100_000,
			expectedCost: 30.00 + 6.00, // 36.00
		},
		// DeepSeek v3 alias
		{
			model:           "deepseek-v3",
			inputTokens:     1_000_000,
			outputTokens:    100_000,
			cacheReadTokens: 1_000_000,
			expectedCost:    0.14 + 0.028 + 0.014, // 0.182
		},
		// DeepSeek r1 alias
		{
			model:           "deepseek-r1",
			inputTokens:     1_000_000,
			outputTokens:    100_000,
			cacheReadTokens: 1_000_000,
			expectedCost:    0.55 + 0.219 + 0.14, // 0.909
		},
		// DeepSeek coder
		{
			model:        "deepseek-coder",
			inputTokens:  1_000_000,
			outputTokens: 100_000,
			expectedCost: 0.14 + 0.028, // 0.168
		},
	}

	for _, tt := range tests {
		t.Run(tt.model, func(t *testing.T) {
			cost, ok := e.CalculateCost(tt.model, tt.inputTokens, tt.outputTokens, tt.cacheReadTokens, tt.cacheWriteTokens)
			if !ok || cost == nil {
				t.Fatalf("expected model %q to match and return cost", tt.model)
			}
			diff := *cost - tt.expectedCost
			if diff < -0.001 || diff > 0.001 {
				t.Fatalf("model %q: expected cost ~%f, got %f", tt.model, tt.expectedCost, *cost)
			}
		})
	}
}

