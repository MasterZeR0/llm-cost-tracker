package pricing

// DefaultModelPrices contains fallback token pricing per million tokens
var DefaultModelPrices = map[string]ModelPrice{
	// Anthropic Claude Family
	// Newer Opus generation (Claude Opus 4.5, 4.6, 4.7, 4.8)
	"claude-opus-4.[5-9]*": {
		InputPerMTok:      5.00,
		OutputPerMTok:     25.00,
		CacheReadPerMTok:  0.50,
		CacheWritePerMTok: 6.25,
	},
	"claude-opus-4-[5-9]*": {
		InputPerMTok:      5.00,
		OutputPerMTok:     25.00,
		CacheReadPerMTok:  0.50,
		CacheWritePerMTok: 6.25,
	},
	"claude-opus-5*": {
		InputPerMTok:      4.00,
		OutputPerMTok:     20.00,
		CacheReadPerMTok:  0.40,
		CacheWritePerMTok: 5.00,
	},
	// Legacy / Base Claude 3/4 Opus
	"claude-3-opus*": {
		InputPerMTok:      15.00,
		OutputPerMTok:     75.00,
		CacheReadPerMTok:  1.50,
		CacheWritePerMTok: 18.75,
	},
	"claude-opus*": {
		InputPerMTok:      15.00,
		OutputPerMTok:     75.00,
		CacheReadPerMTok:  1.50,
		CacheWritePerMTok: 18.75,
	},
	"claude-3-5-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-3-7-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-sonnet*": {
		InputPerMTok:      3.00,
		OutputPerMTok:     15.00,
		CacheReadPerMTok:  0.30,
		CacheWritePerMTok: 3.75,
	},
	"claude-3-5-haiku*": {
		InputPerMTok:      0.80,
		OutputPerMTok:     4.00,
		CacheReadPerMTok:  0.08,
		CacheWritePerMTok: 1.00,
	},
	"claude-3-haiku*": {
		InputPerMTok:      0.80,
		OutputPerMTok:     4.00,
		CacheReadPerMTok:  0.08,
		CacheWritePerMTok: 1.00,
	},
	"claude-haiku*": {
		InputPerMTok:      0.80,
		OutputPerMTok:     4.00,
		CacheReadPerMTok:  0.08,
		CacheWritePerMTok: 1.00,
	},

	// OpenAI GPT-4.5 Family
	"gpt-4.5*": {
		InputPerMTok:     75.00,
		OutputPerMTok:    150.00,
		CacheReadPerMTok: 37.50,
	},

	// OpenAI GPT-4o Family
	"gpt-4o": {
		InputPerMTok:     2.50,
		OutputPerMTok:    10.00,
		CacheReadPerMTok: 1.25,
	},
	"gpt-4o-mini": {
		InputPerMTok:     0.15,
		OutputPerMTok:    0.60,
		CacheReadPerMTok: 0.075,
	},

	// OpenAI GPT-4 Turbo & Base
	"gpt-4-turbo*": {
		InputPerMTok:  10.00,
		OutputPerMTok: 30.00,
	},
	"gpt-4": {
		InputPerMTok:  30.00,
		OutputPerMTok: 60.00,
	},

	// OpenAI Reasoning / o-Series Models
	"o1-mini*": {
		InputPerMTok:     1.10,
		OutputPerMTok:    4.40,
		CacheReadPerMTok: 0.55,
	},
	"o1*": {
		InputPerMTok:     15.00,
		OutputPerMTok:    60.00,
		CacheReadPerMTok: 7.50,
	},
	"o3-mini*": {
		InputPerMTok:     1.10,
		OutputPerMTok:    4.40,
		CacheReadPerMTok: 0.55,
	},
	"o3-pro*": {
		InputPerMTok:  20.00,
		OutputPerMTok: 80.00,
	},
	"o3*": {
		InputPerMTok:     2.00,
		OutputPerMTok:    8.00,
		CacheReadPerMTok: 0.50,
	},

	// Google Gemini Family
	"gemini-3*flash*": {
		InputPerMTok:  0.10,
		OutputPerMTok: 0.40,
	},
	"gemini-3*pro*": {
		InputPerMTok:  1.25,
		OutputPerMTok: 5.00,
	},
	"gemini-2*flash*": {
		InputPerMTok:  0.10,
		OutputPerMTok: 0.40,
	},
	"gemini-1.5-flash*": {
		InputPerMTok:  0.075,
		OutputPerMTok: 0.30,
	},
	"gemini-2.5-pro*": {
		InputPerMTok:     1.25,
		OutputPerMTok:    10.00,
		CacheReadPerMTok: 0.125,
	},
	"gemini-2*pro*": {
		InputPerMTok:  1.25,
		OutputPerMTok: 5.00,
	},
	"gemini-1.5-pro*": {
		InputPerMTok:  1.25,
		OutputPerMTok: 5.00,
	},

	// DeepSeek Models
	"deepseek-chat*": {
		InputPerMTok:     0.14,
		OutputPerMTok:    0.28,
		CacheReadPerMTok: 0.014,
	},
	"deepseek-v3*": {
		InputPerMTok:     0.14,
		OutputPerMTok:    0.28,
		CacheReadPerMTok: 0.014,
	},
	"deepseek-coder*": {
		InputPerMTok:     0.14,
		OutputPerMTok:    0.28,
		CacheReadPerMTok: 0.014,
	},
	"deepseek-reasoner*": {
		InputPerMTok:     0.55,
		OutputPerMTok:    2.19,
		CacheReadPerMTok: 0.14,
	},
	"deepseek-r1*": {
		InputPerMTok:     0.55,
		OutputPerMTok:    2.19,
		CacheReadPerMTok: 0.14,
	},

	// Zero cache discount models (input price == cache read price)
	"zero-discount-model*": {
		InputPerMTok:     3.00,
		OutputPerMTok:    15.00,
		CacheReadPerMTok: 3.00,
	},
}
