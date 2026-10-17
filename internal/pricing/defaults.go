package pricing

// DefaultModelPrices contains fallback token pricing per million tokens
var DefaultModelPrices = map[string]ModelPrice{
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
	"gpt-4o": {
		InputPerMTok:  2.50,
		OutputPerMTok: 10.00,
	},
	"gpt-4o-mini": {
		InputPerMTok:  0.15,
		OutputPerMTok: 0.60,
	},
	"gemini-2*flash*": {
		InputPerMTok:  0.10,
		OutputPerMTok: 0.40,
	},
	"gemini-2*pro*": {
		InputPerMTok:  1.25,
		OutputPerMTok: 5.00,
	},
}
