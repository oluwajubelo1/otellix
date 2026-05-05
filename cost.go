package otellix

import (
	"sync"

	"github.com/oluwajubelo1/otellix/providers"
)

// PricingEntry holds the per-million-token pricing for a specific model.
type PricingEntry struct {
	InputPricePerMToken      float64 // USD per 1 million input tokens
	OutputPricePerMToken     float64 // USD per 1 million output tokens
	CacheReadPricePerMToken  float64 // USD per 1 million cached tokens read (hits)
	CacheWritePricePerMToken float64 // USD per 1 million cached tokens written (creation)
}

// pricingTable holds the default pricing keyed by "provider/model".
// Protected by pricingMu for concurrent RegisterModel calls.
var (
	pricingMu    sync.RWMutex
	pricingTable = map[string]PricingEntry{
		// --- Anthropic ---
		"anthropic/claude-opus-4-6":   {InputPricePerMToken: 15.0, OutputPricePerMToken: 75.0, CacheReadPricePerMToken: 1.50, CacheWritePricePerMToken: 18.75},
		"anthropic/claude-sonnet-4-6": {InputPricePerMToken: 3.0, OutputPricePerMToken: 15.0, CacheReadPricePerMToken: 0.30, CacheWritePricePerMToken: 3.75},
		"anthropic/claude-haiku-4-5":  {InputPricePerMToken: 0.80, OutputPricePerMToken: 4.0, CacheReadPricePerMToken: 0.08, CacheWritePricePerMToken: 1.00},

		// --- OpenAI ---
		"openai/gpt-4o":      {InputPricePerMToken: 2.50, OutputPricePerMToken: 10.0, CacheReadPricePerMToken: 1.25, CacheWritePricePerMToken: 2.50},
		"openai/gpt-4o-mini": {InputPricePerMToken: 0.15, OutputPricePerMToken: 0.60, CacheReadPricePerMToken: 0.075, CacheWritePricePerMToken: 0.15},

		// --- Google Gemini ---
		"gemini/gemini-2.5-pro":   {InputPricePerMToken: 1.25, OutputPricePerMToken: 10.0, CacheReadPricePerMToken: 0.31, CacheWritePricePerMToken: 1.25},
		"gemini/gemini-2.5-flash": {InputPricePerMToken: 0.075, OutputPricePerMToken: 0.30, CacheReadPricePerMToken: 0.018, CacheWritePricePerMToken: 0.075},

		// --- Ollama (local, free) ---
		"ollama/any": {InputPricePerMToken: 0, OutputPricePerMToken: 0, CacheReadPricePerMToken: 0, CacheWritePricePerMToken: 0},
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
	readCacheCost := float64(result.CacheReadTokens) * entry.CacheReadPricePerMToken / 1_000_000
	writeCacheCost := float64(result.CacheWriteTokens) * entry.CacheWritePricePerMToken / 1_000_000

	return inputCost + outputCost + readCacheCost + writeCacheCost
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

// ListPricing returns a snapshot of all known provider/model pricing entries.
// The returned map's keys are in "provider/model" format (e.g., "anthropic/claude-sonnet-4-6").
// This is useful for analyzing pricing data and suggesting model alternatives.
func ListPricing() map[string]PricingEntry {
	pricingMu.RLock()
	defer pricingMu.RUnlock()

	out := make(map[string]PricingEntry, len(pricingTable))
	for k, v := range pricingTable {
		out[k] = v
	}
	return out
}
