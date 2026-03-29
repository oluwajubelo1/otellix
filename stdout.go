package otellix

import (
	"fmt"

	"github.com/oluwajubelo1/otellix/providers"
)

// defaultDevPrinter prints a human-readable summary of each LLM call to stdout.
// This is the callback used when SetupDev() or RegisterDevPrinter() is called.
func defaultDevPrinter(cfg *Config, result *providers.CallResult, costUSD, latencyMs float64, enforcer *BudgetEnforcer) {
	latencyStr := formatLatency(latencyMs)

	// Line 1: provider/model | feature | user
	fmt.Printf("[otellix] %s/%s", cfg.Provider, cfg.Model)
	if cfg.FeatureID != "" {
		fmt.Printf(" | feature: %s", cfg.FeatureID)
	}
	if cfg.UserID != "" {
		fmt.Printf(" | user: %s", cfg.UserID)
	}
	fmt.Println()

	// Line 2: tokens | cost | latency
	fmt.Printf("          tokens: %d in + %d out", result.InputTokens, result.OutputTokens)
	if result.CachedTokens > 0 {
		fmt.Printf(" (%d cached)", result.CachedTokens)
	}
	fmt.Printf(" | cost: $%.6f | latency: %s\n", costUSD, latencyStr)

	// Line 3: fingerprint | budget (if applicable)
	hasLine3 := false
	if cfg.EnablePromptFingerprint && cfg.PromptText != "" {
		fp := promptFingerprint(cfg.PromptText)
		fmt.Printf("          prompt_fingerprint: %s", fp)
		hasLine3 = true
	}

	if enforcer != nil && cfg.UserID != "" {
		remaining := enforcer.Remaining(cfg.UserID, cfg.ProjectID)
		var limit float64
		if enforcer.config.PerUserDailyLimit > 0 {
			limit = enforcer.config.PerUserDailyLimit
		} else if enforcer.config.PerProjectDailyLimit > 0 {
			limit = enforcer.config.PerProjectDailyLimit
		}
		if limit > 0 {
			spent := limit - remaining
			pct := (spent / limit) * 100
			if hasLine3 {
				fmt.Printf(" | ")
			} else {
				fmt.Printf("          ")
			}
			fmt.Printf("budget: $%.3f/$%.3f (%.0f%%)", spent, limit, pct)
			hasLine3 = true
		}
	}

	if hasLine3 {
		fmt.Println()
	}
	fmt.Println() // blank line separator
}

// formatLatency formats milliseconds into a human-readable duration.
func formatLatency(ms float64) string {
	if ms < 1000 {
		return fmt.Sprintf("%.0fms", ms)
	}
	return fmt.Sprintf("%.1fs", ms/1000)
}
