// examples/basic/main.go — Minimal working example of Otellix.
//
// Run with:
//
//	ANTHROPIC_API_KEY=xxx go run examples/basic/main.go
//
// If ANTHROPIC_API_KEY is not set, a mock provider is used so the example
// runs anywhere without network access.
package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
	shutdown := otellix.SetupDev()
	defer shutdown()

	var provider providers.Provider
	if os.Getenv("ANTHROPIC_API_KEY") != "" {
		provider = anthropic.New()
	} else {
		fmt.Println("ANTHROPIC_API_KEY not set — using mock provider")
		provider = &providers.MockProvider{
			ProviderName: "anthropic",
			Result: providers.CallResult{
				InputTokens: 50, OutputTokens: 120,
				CacheReadTokens: 10, Model: "claude-sonnet-4-6",
			},
		}
	}

	result, err := otellix.Trace(
		context.Background(),
		provider,
		providers.CallParams{
			Model:        "claude-sonnet-4-6",
			MaxTokens:    256,
			SystemPrompt: "You are a helpful assistant. Be concise.",
			Messages: []providers.Message{
				{Role: "user", Content: "What is OpenTelemetry in one sentence?"},
			},
		},
		otellix.WithFeatureID("demo"),
		otellix.WithUserID("usr_001"),
		otellix.WithProjectID("otellix-example"),
		otellix.WithPromptFingerprint("You are a helpful assistant. Be concise.What is OpenTelemetry in one sentence?"),
	)
	if err != nil {
		log.Fatalf("LLM call failed: %v", err)
	}

	// The stdout dev printer already printed the trace summary.
	// Here we also print the raw response text.
	fmt.Printf("Response tokens: %d in, %d out\n", result.InputTokens, result.OutputTokens)
}
