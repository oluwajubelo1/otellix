package tests

import (
	"context"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
)

// TestSetupDevConsoleLeavesTracerProviderUntouched verifies that the quiet dev
// mode does NOT replace the global TracerProvider (unlike SetupDev, which wires
// up an OTel stdout trace exporter).
func TestSetupDevConsoleLeavesTracerProviderUntouched(t *testing.T) {
	before := otel.GetTracerProvider()

	shutdown := otellix.SetupDevConsole()
	defer shutdown()

	if otel.GetTracerProvider() != before {
		t.Error("SetupDevConsole must not replace the global TracerProvider")
	}
}

// TestSetupDevConsoleTraceStillWorks verifies that tracing and cost calculation
// keep working after the quiet dev mode is enabled (the dev printer is invoked
// for every call and must not panic).
func TestSetupDevConsoleTraceStillWorks(t *testing.T) {
	shutdown := otellix.SetupDevConsole()
	defer shutdown()

	mock := &providers.MockProvider{
		ProviderName: "anthropic",
		Result: providers.CallResult{
			InputTokens: 100, OutputTokens: 50, CacheReadTokens: 10,
			Model: "claude-sonnet-4-6",
		},
	}

	result, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithUserID("console-user"),
	)
	if err != nil {
		t.Fatalf("expected success, got error: %v", err)
	}
	if result.InputTokens != 100 || result.OutputTokens != 50 {
		t.Errorf("unexpected token counts: %d in, %d out", result.InputTokens, result.OutputTokens)
	}
}
