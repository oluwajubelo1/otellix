package main

import (
	"context"
	"fmt"
	"log"
	"os"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
	// 1. Setup a budget enforcer with a low limit.
	bc := &otellix.BudgetConfig{
		PerUserLimit:   0.10, // $0.10 limit
		FallbackAction: otellix.FallbackNotify,
		ResetInterval:  24 * time.Hour,
	}
	enforcer := otellix.NewBudgetEnforcer(bc)

	// 2. Setup Otellix with a Prompt Decorator.
	// This decorator will inject budget context directly into the system prompt.
	decorator := func(ctx context.Context, status otellix.BudgetStatus, params *providers.CallParams) {
		budgetMsg := fmt.Sprintf("\n[BUDGET CONTEXT: You have $%.4f remaining for today. Please be extremely concise.]", status.Remaining)
		params.SystemPrompt += budgetMsg

		// We can also dynamically adjust max tokens based on budget
		if status.Remaining < 0.02 {
			params.MaxTokens = 50
		}
	}

	// 3. Setup Provider.
	// If ANTHROPIC_API_KEY is set, use the real Anthropic provider.
	// Otherwise fall back to a mock provider so the demo runs anywhere.
	var p providers.Provider
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey != "" {
		p = anthropic.New()
	} else {
		fmt.Println("ANTHROPIC_API_KEY not set — using mock provider")
		p = &providers.MockProvider{
			ProviderName: "anthropic",
			Result: providers.CallResult{
				InputTokens: 100, OutputTokens: 50,
				CacheReadTokens: 20, Model: "claude-3-5-sonnet-20240620",
			},
		}
	}

	// 4. Trace the call.
	ctx := context.Background()
	params := providers.CallParams{
		Model:        "claude-3-5-sonnet-20240620",
		SystemPrompt: "You are a helpful assistant.",
		Messages: []providers.Message{
			{Role: "user", Content: "Explain quantum computing in one sentence."},
		},
		MaxTokens: 1024,
	}

	res, err := otellix.Trace(ctx, p, params,
		otellix.WithBudgetConfig(bc),
		otellix.WithPromptDecorator(decorator),
		otellix.WithUserID("user_456"),
	)
	if err != nil {
		log.Fatalf("Trace failed: %v", err)
	}

	fmt.Printf("Tokens: %d in, %d out\n", res.InputTokens, res.OutputTokens)

	// Show what the provider actually received (decorator mutations are applied
	// inside Trace to the call params).
	if mock, ok := p.(*providers.MockProvider); ok {
		fmt.Printf("Prompt received by provider: %s\n", mock.LastParams.SystemPrompt)
	} else {
		fmt.Printf("Decorated System Prompt used: %s\n", params.SystemPrompt)
	}

	status := enforcer.Status(ctx, "user_456", "")
	fmt.Printf("Final Budget Remaining: $%.4f\n", status.Remaining)
}
