package tokenestimator

import "testing"

func TestEstimateTokens(t *testing.T) {
	prompt := "Write a fast go function to calculate fibonacci numbers."
	response := "Here is a clean implementation of Fibonacci in Go:\n\nfunc Fib(n int) int {\n    if n <= 1 { return n }\n    return Fib(n-1) + Fib(n-2)\n}"

	inTokens, outTokens := EstimateTokens(prompt, response)
	if inTokens <= 0 {
		t.Errorf("expected positive input tokens, got %d", inTokens)
	}
	if outTokens <= 0 {
		t.Errorf("expected positive output tokens, got %d", outTokens)
	}
	if inTokens >= int64(len(prompt)) {
		t.Errorf("input tokens should be smaller than char length: tokens=%d, chars=%d", inTokens, len(prompt))
	}

	// Empty strings
	in0, out0 := EstimateTokens("", "")
	if in0 != 0 || out0 != 0 {
		t.Errorf("expected 0, 0 for empty strings, got %d, %d", in0, out0)
	}
}
