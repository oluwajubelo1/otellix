package otellix

import (
	"sync"

	"github.com/oluwajubelo1/otellix/providers"
)

// PricingEntry holds the per-million-token pricing for a specific model.
type PricingEntry struct {
	InputPricePerMToken  float64 // USD per 1 million input tokens
	OutputPricePerMToken float64 // USD per 1 million output tokens
	CachePricePerMToken  float64 // USD per 1 million cached tokens (0 if N/A)
}

// pricingTable holds the default pricing keyed by "provider/model".
// Protected by pricingMu for concurrent RegisterModel calls.
var (
	pricingMu    sync.RWMutex
	pricingTable = map[string]PricingEntry{
		// --- Anthropic ---
		"anthropic/claude-opus-4-6":   {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CachePricePerMToken: 1.50},
		"anthropic/claude-sonnet-4-6": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CachePricePerMToken: 0.30},
		"anthropic/claude-haiku-4-5":  {InputPricePerMToken: 0.80, OutputPricePerMToken: 4.0, CachePricePerMToken: 0.08},

		// --- OpenAI ---
		"openai/gpt-4o":      {InputPricePerMToken: 2.50, OutputPricePerMToken: 10.0, CachePricePerMToken: 1.25},
		"openai/gpt-4o-mini": {InputPricePerMToken: 0.15, OutputPricePerMToken: 0.60, CachePricePerMToken: 0.075},

		// --- Google Gemini ---
		"gemini/gemini-2.5-pro":   {InputPricePerMToken: 1.25, OutputPricePerMToken: 10.0, CachePricePerMToken: 0.31},
		"gemini/gemini-2.5-flash": {InputPricePerMToken: 0.075, OutputPricePerMToken: 0.30, CachePricePerMToken: 0.018},

		// --- Ollama (local, free) ---
		"ollama/any": {InputPricePerMToken: 0, OutputPricePerMToken: 0, CachePricePerMToken: 0},
	}
)

// CalculateCost computes the USD cost for an LLM call based on the provider,
// model, and token counts in the result.
//
// Returns 0 for unknown provider/model combinations (does not panic).
func CalculateCost(provider, model string, result providers.CallResult) float64 {
	entry, ok := GetPricing(provider, model)
	if !ok {
		// Check for wildcard ollama pricing.
		if provider == "ollama" {
			return 0
		}
		return 0
	}

	inputCost := float64(result.InputTokens) * entry.InputPricePerMToken / 1_000_000
	outputCost := float64(result.OutputTokens) * entry.OutputPricePerMToken / 1_000_000
	cacheCost := float64(result.CachedTokens) * entry.CachePricePerMToken / 1_000_000

	return inputCost + outputCost + cacheCost
}

// EstimateCost estimates the cost assuming the given number of output tokens.
// Used for pre-call budget checks where actual token counts are unknown.
func EstimateCost(provider, model string, estimatedOutputTokens int64) float64 {
	entry, ok := GetPricing(provider, model)
	if !ok {
		if provider == "ollama" {
			return 0
		}
		return 0
	}
	// Rough estimate: assume input tokens ≈ output tokens for estimation.
	return float64(estimatedOutputTokens) * (entry.InputPricePerMToken + entry.OutputPricePerMToken) / 1_000_000
}

// GetPricing returns the pricing entry for a specific provider/model combination.
// Returns (entry, false) if not found.
func GetPricing(provider, model string) (PricingEntry, bool) {
	key := provider + "/" + model
	pricingMu.RLock()
	defer pricingMu.RUnlock()

	entry, ok := pricingTable[key]
	if !ok {
		// Try ollama wildcard.
		if provider == "ollama" {
			entry, ok = pricingTable["ollama/any"]
		}
	}
	return entry, ok
}

// RegisterModel adds or updates pricing for a provider/model combination.
// This allows callers to add custom models or override built-in pricing.
//
// Example:
//
//	otellix.RegisterModel("anthropic", "claude-4-opus", otellix.PricingEntry{
//	    InputPricePerMToken:  20.0,
//	    OutputPricePerMToken: 100.0,
//	})
func RegisterModel(provider, model string, entry PricingEntry) {
	key := provider + "/" + model
	pricingMu.Lock()
	defer pricingMu.Unlock()
	pricingTable[key] = entry
}
