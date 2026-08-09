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
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*5.0 + 500.0*25.0) / 1_000_000,
		},
		{
			name:     "anthropic/claude-sonnet-4-6",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 200, CacheWriteTokens: 0},
			wantCost: (1000.0*3.0 + 500.0*15.0 + 200.0*0.30) / 1_000_000,
		},
		{
			name:     "anthropic/claude-haiku-4-5",
			provider: "anthropic",
			model:    "claude-haiku-4-5",
			result:   providers.CallResult{InputTokens: 5000, OutputTokens: 1000, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (5000.0*0.80 + 1000.0*4.0) / 1_000_000,
		},
		// --- OpenAI ---
		{
			name:     "openai/gpt-4o",
			provider: "openai",
			model:    "gpt-4o",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 100, CacheWriteTokens: 0},
			wantCost: (1000.0*2.50 + 500.0*10.0 + 100.0*1.25) / 1_000_000,
		},
		{
			name:     "openai/gpt-4o-mini",
			provider: "openai",
			model:    "gpt-4o-mini",
			result:   providers.CallResult{InputTokens: 2000, OutputTokens: 800, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (2000.0*0.15 + 800.0*0.60) / 1_000_000,
		},
		// --- Gemini ---
		{
			name:     "gemini/gemini-2.5-pro",
			provider: "gemini",
			model:    "gemini-2.5-pro",
			result:   providers.CallResult{InputTokens: 3000, OutputTokens: 1000, CacheReadTokens: 500, CacheWriteTokens: 0},
			wantCost: (3000.0*1.25 + 1000.0*10.0 + 500.0*0.31) / 1_000_000,
		},
		{
			name:     "gemini/gemini-2.5-flash",
			provider: "gemini",
			model:    "gemini-2.5-flash",
			result:   providers.CallResult{InputTokens: 10000, OutputTokens: 2000, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (10000.0*0.30 + 2000.0*2.50) / 1_000_000,
		},
		// --- OpenAI newer ---
		{
			name:     "openai/gpt-5.4",
			provider: "openai",
			model:    "gpt-5.4",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*2.50 + 500.0*15.0) / 1_000_000,
		},
		{
			name:     "openai/gpt-5-mini",
			provider: "openai",
			model:    "gpt-5-mini",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.25 + 500.0*2.0) / 1_000_000,
		},
		{
			name:     "openai/o3",
			provider: "openai",
			model:    "o3",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*2.0 + 500.0*8.0) / 1_000_000,
		},
		{
			name:     "openai/gpt-oss-120b",
			provider: "openai",
			model:    "gpt-oss-120b",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.30 + 500.0*1.20) / 1_000_000,
		},
		// --- Anthropic newer ---
		{
			name:     "anthropic/claude-sonnet-5",
			provider: "anthropic",
			model:    "claude-sonnet-5",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*3.0 + 500.0*15.0) / 1_000_000,
		},
		{
			name:     "anthropic/claude-opus-5",
			provider: "anthropic",
			model:    "claude-opus-5",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*5.0 + 500.0*25.0) / 1_000_000,
		},
		{
			name:     "anthropic/claude-haiku-3",
			provider: "anthropic",
			model:    "claude-haiku-3",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.25 + 500.0*1.25) / 1_000_000,
		},
		// --- Gemini newer ---
		{
			name:     "gemini/gemini-3.6-flash",
			provider: "gemini",
			model:    "gemini-3.6-flash",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*1.50 + 500.0*7.50) / 1_000_000,
		},
		{
			name:     "gemini/gemini-3.1-pro",
			provider: "gemini",
			model:    "gemini-3.1-pro",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*2.0 + 500.0*12.0) / 1_000_000,
		},
		// --- Kimi (Moonshot) ---
		{
			name:     "kimi/kimi-k3",
			provider: "kimi",
			model:    "kimi-k3",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*3.0 + 500.0*15.0) / 1_000_000,
		},
		{
			name:     "kimi/kimi-k2.6",
			provider: "kimi",
			model:    "kimi-k2.6",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.95 + 500.0*4.0) / 1_000_000,
		},
		{
			name:     "kimi/kimi-k2",
			provider: "kimi",
			model:    "kimi-k2",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.60 + 500.0*2.50) / 1_000_000,
		},
		// --- DeepSeek ---
		{
			name:     "deepseek/deepseek-v4",
			provider: "deepseek",
			model:    "deepseek-v4",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.30 + 500.0*0.50) / 1_000_000,
		},
		{
			name:     "deepseek/deepseek-r1",
			provider: "deepseek",
			model:    "deepseek-r1",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.55 + 500.0*2.19) / 1_000_000,
		},
		{
			name:     "deepseek/deepseek-chat",
			provider: "deepseek",
			model:    "deepseek-chat",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.28 + 500.0*0.42) / 1_000_000,
		},
		// --- Qwen ---
		{
			name:     "qwen/qwen3.5-397b",
			provider: "qwen",
			model:    "qwen3.5-397b",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.60 + 500.0*3.60) / 1_000_000,
		},
		{
			name:     "qwen/qwen3.7-flash",
			provider: "qwen",
			model:    "qwen3.7-flash",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.03 + 500.0*0.13) / 1_000_000,
		},
		// --- GLM ---
		{
			name:     "glm/glm-5.2",
			provider: "glm",
			model:    "glm-5.2",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*1.40 + 500.0*4.40) / 1_000_000,
		},
		{
			name:     "glm/glm-4.5-air",
			provider: "glm",
			model:    "glm-4.5-air",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.20 + 500.0*1.10) / 1_000_000,
		},
		// --- ERNIE ---
		{
			name:     "ernie/ernie-4.5",
			provider: "ernie",
			model:    "ernie-4.5",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.55 + 500.0*2.20) / 1_000_000,
		},
		// --- Hunyuan ---
		{
			name:     "hunyuan/hunyuan-hy3",
			provider: "hunyuan",
			model:    "hunyuan-hy3",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*0.063 + 500.0*0.21) / 1_000_000,
		},
		// --- Mistral ---
		{
			name:     "mistral/mistral-large",
			provider: "mistral",
			model:    "mistral-large",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*2.0 + 500.0*6.0) / 1_000_000,
		},
		// --- Grok ---
		{
			name:     "grok/grok-4",
			provider: "grok",
			model:    "grok-4",
			result:   providers.CallResult{InputTokens: 1000, OutputTokens: 500, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: (1000.0*3.0 + 500.0*15.0) / 1_000_000,
		},
		// --- Ollama (free) ---
		{
			name:     "ollama/llama3",
			provider: "ollama",
			model:    "llama3",
			result:   providers.CallResult{InputTokens: 5000, OutputTokens: 2000, CacheReadTokens: 0, CacheWriteTokens: 0},
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
			result:   providers.CallResult{InputTokens: 0, OutputTokens: 0, CacheReadTokens: 0, CacheWriteTokens: 0},
			wantCost: 0,
		},
		{
			name:     "cached tokens only",
			provider: "anthropic",
			model:    "claude-sonnet-4-6",
			result:   providers.CallResult{InputTokens: 0, OutputTokens: 0, CacheReadTokens: 1000, CacheWriteTokens: 0},
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
		InputPricePerMToken:      5.0,
		OutputPricePerMToken:     25.0,
		CacheReadPricePerMToken:  0.5,
		CacheWritePricePerMToken: 1.0,
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
		InputTokens: 1_000_000, OutputTokens: 1_000_000, CacheReadTokens: 1_000_000, CacheWriteTokens: 1_000_000,
	})
	want := 5.0 + 25.0 + 0.5 + 1.0
	if diff := cost - want; diff > 1e-9 || diff < -1e-9 {
		t.Errorf("cost = %f, want %f", cost, want)
	}
}
