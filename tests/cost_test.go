package tests

import (
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

func TestCalculateCost(t *testing.T) {
	tests := []struct {
		name     string
		provider string
		model    string
		result   providers.CallResult
		wantCost float64
	}{
		// --- Anthropic ---
		{
			name:     "anthropic/claude-opus-4-6",
			provider: "anthropic",
			model:    "claude-opus-4-6",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CachedTokens: 0},
			wantCost: (1000.0*15.0 + 500.0*75.0) / 1_000_000,
		},
		{
			name:     "anthropic/claude-sonnet-4-6",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CachedTokens: 200},
			wantCost: (1000.0*3.0 + 500.0*15.0 + 200.0*0.30) / 1_000_000,
		},
		{
			name:     "anthropic/claude-haiku-4-5",
			provider: "anthropic",
			model:    "claude-haiku-4-5",
			result:   providers.CallResult{InputTokens: 5000, OutputTokens: 1000, CachedTokens: 0},
			wantCost: (5000.0*0.80 + 1000.0*4.0) / 1_000_000,
		},
		// --- OpenAI ---
		{
			name:     "openai/gpt-4o",
			provider: "openai",
			model:    "gpt-4o",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CachedTokens: 100},
			wantCost: (1000.0*2.50 + 500.0*10.0 + 100.0*1.25) / 1_000_000,
		},
		{
			name:     "openai/gpt-4o-mini",
			provider: "openai",
			model:    "gpt-4o-mini",
			result:   providers.CallResult{InputTokens: 2000, OutputTokens: 800, CachedTokens: 0},
			wantCost: (2000.0*0.15 + 800.0*0.60) / 1_000_000,
		},
		// --- Gemini ---
		{
			name:     "gemini/gemini-2.5-pro",
			provider: "gemini",
			model:    "gemini-2.5-pro",
			result:   providers.CallResult{InputTokens: 3000, OutputTokens: 1000, CachedTokens: 500},
			wantCost: (3000.0*1.25 + 1000.0*10.0 + 500.0*0.31) / 1_000_000,
		},
		{
			name:     "gemini/gemini-2.5-flash",
			provider: "gemini",
			model:    "gemini-2.5-flash",
			result:   providers.CallResult{InputTokens: 10000, OutputTokens: 2000, CachedTokens: 0},
			wantCost: (10000.0*0.075 + 2000.0*0.30) / 1_000_000,
		},
		// --- Ollama (free) ---
		{
			name:     "ollama/llama3",
			provider: "ollama",
			model:    "llama3",
			result:   providers.CallResult{InputTokens: 5000, OutputTokens: 2000, CachedTokens: 0},
			wantCost: 0,
		},
		// --- Edge cases ---
		{
			name:     "unknown provider/model returns 0",
			provider: "unknown",
			model:    "nonexistent",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500},
			wantCost: 0,
		},
		{
			name:     "zero tokens",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			result:   providers.CallResult{InputTokens: 0, OutputTokens: 0, CachedTokens: 0},
			wantCost: 0,
		},
		{
			name:     "cached tokens only",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			result:   providers.CallResult{InputTokens: 0, OutputTokens: 0, CachedTokens: 1000},
			wantCost: (1000.0 * 0.30) / 1_000_000,
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := otellix.CalculateCost(tt.provider, tt.model, tt.result)
			// Use approximate comparison for floating point.
			if diff := got - tt.wantCost; diff > 1e-12 || diff < -1e-12 {
				t.Errorf("CalculateCost(%s, %s) = %.12f, want %.12f",
					tt.provider, tt.model, got, tt.wantCost)
			}
		})
	}
}

func TestGetPricing(t *testing.T) {
	t.Run("known model returns entry", func(t *testing.T) {
		entry, ok := otellix.GetPricing("anthropic", "claude-sonnet-4-6")
		if !ok {
			t.Fatal("expected to find pricing for anthropic/claude-sonnet-4-6")
		}
		if entry.InputPricePerMToken != 3.0 {
			t.Errorf("input price = %f, want 3.0", entry.InputPricePerMToken)
		}
	})

	t.Run("unknown model returns false", func(t *testing.T) {
		_, ok := otellix.GetPricing("unknown", "model")
		if ok {
			t.Error("expected false for unknown model")
		}
	})

	t.Run("ollama wildcard", func(t *testing.T) {
		entry, ok := otellix.GetPricing("ollama", "custom-model")
		if !ok {
			t.Fatal("expected ollama wildcard to match")
		}
		if entry.InputPricePerMToken != 0 {
			t.Errorf("ollama should be free, got input price %f", entry.InputPricePerMToken)
		}
	})
}

func TestRegisterModel(t *testing.T) {
	otellix.RegisterModel("custom", "my-model", otellix.PricingEntry{
		InputPricePerMToken:  5.0,
		OutputPricePerMToken: 25.0,
		CachePricePerMToken:  0.5,
	})

	entry, ok := otellix.GetPricing("custom", "my-model")
	if !ok {
		t.Fatal("expected to find custom model pricing")
	}
	if entry.InputPricePerMToken != 5.0 {
		t.Errorf("input price = %f, want 5.0", entry.InputPricePerMToken)
	}

	// Test cost calculation with custom model.
	cost := otellix.CalculateCost("custom", "my-model", providers.CallResult{
		InputTokens: 1_000_000, OutputTokens: 1_000_000, CachedTokens: 1_000_000,
	})
	want := 5.0 + 25.0 + 0.5
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}
