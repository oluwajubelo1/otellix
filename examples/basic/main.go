// examples/basic/main.go — Minimal working example of Otellix.
//
// Run with:
//
//	ANTHROPIC_API_KEY=xxx go run examples/basic/main.go
package main

import (
	"context"
	"fmt"
	"log"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
	shutdown := otellix.SetupDev()
	defer shutdown()

	provider := anthropic.New()

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
