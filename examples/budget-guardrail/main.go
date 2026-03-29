// examples/budget-guardrail/main.go — Budget enforcement demonstration.
//
// This example sets a $0.05/day per-user budget ceiling and makes multiple
// LLM calls to show the budget being enforced.
//
// Run with:
//
//	ANTHROPIC_API_KEY=xxx go run examples/budget-guardrail/main.go
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
	shutdown := otellix.SetupDev()
	defer shutdown()

	provider := anthropic.New()

	budgetCfg := &otellix.BudgetConfig{
		PerUserDailyLimit:    0.05,
		PerProjectDailyLimit: 2.00,
		FallbackAction:       otellix.FallbackBlock,
		ResetInterval:        24 * time.Hour,
	}

	fmt.Println("=== Otellix Budget Guardrail Demo ===")
	fmt.Println("Budget: $0.05/day per user, $2.00/day per project")
	fmt.Println("Making 5 LLM calls from the same user...")
	fmt.Println()

	for i := 1; i <= 5; i++ {
		fmt.Printf("--- Call %d ---\n", i)

		result, err := otellix.Trace(
			context.Background(),
			provider,
			providers.CallParams{
				Model:        "claude-sonnet-4-6",
				MaxTokens:    512,
				SystemPrompt: "You are a helpful assistant.",
				Messages: []providers.Message{
					{Role: "user", Content: fmt.Sprintf("Tell me an interesting fact about the number %d.", i)},
				},
			},
			otellix.WithFeatureID("budget-demo"),
			otellix.WithUserID("usr_demo"),
			otellix.WithProjectID("proj_demo"),
			otellix.WithBudgetConfig(budgetCfg),
		)

		if err != nil {
			var budgetErr *otellix.BudgetExceededError
			if errors.As(err, &budgetErr) {
				fmt.Printf("🚫 BLOCKED: %v\n", budgetErr)
				fmt.Printf("   Current spend: $%.4f | Limit: $%.4f | Resets: %s\n",
					budgetErr.CurrentSpend, budgetErr.Limit,
					budgetErr.ResetAt.Format(time.RFC3339))
				continue
			}
			log.Fatalf("Unexpected error: %v", err)
		}

		fmt.Printf("✅ Call succeeded: %d in + %d out tokens\n\n",
			result.InputTokens, result.OutputTokens)
	}

	fmt.Println("=== Demo complete ===")
}
