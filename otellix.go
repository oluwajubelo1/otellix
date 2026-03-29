// Package otellix provides OpenTelemetry-native LLM observability for Go.
//
// Otellix wraps LLM API calls with automatic tracing, token counting,
// cost attribution, and budget enforcement. Built for production Go backends
// with cost-constrained environments as a first-class concern.
//
// Quick start:
//
//	result, err := otellix.Trace(ctx, provider, params,
//	    otellix.WithModel("claude-sonnet-4-6"),
//	    otellix.WithFeatureID("chat-completion"),
//	    otellix.WithUserID("usr_123"),
//	    otellix.WithProjectID("proj_456"),
//	)
package otellix

import (
	"context"

	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/exporters/stdout/stdouttrace"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

// devPrinter is the callback for the stdout dev exporter.
// Set by SetupDev() or registerDevPrinter().
var devPrinter func(cfg *Config, result *providers.CallResult, costUSD, latencyMs float64, enforcer *BudgetEnforcer)

// SetupDev initialises Otellix for local development with a stdout span exporter
// and the human-readable dev printer. Call this in main() for local development.
//
// Usage:
//
//	shutdown := otellix.SetupDev()
//	defer shutdown()
func SetupDev() func() {
	// Set up stdout OTel trace exporter.
	exporter, err := stdouttrace.New(stdouttrace.WithPrettyPrint())
	if err != nil {
		panic("otellix: failed to create stdout exporter: " + err.Error())
	}

	tp := sdktrace.NewTracerProvider(
		sdktrace.WithBatcher(exporter),
	)
	otel.SetTracerProvider(tp)

	// Register the human-readable dev printer.
	RegisterDevPrinter()

	return func() {
		_ = tp.Shutdown(context.Background())
	}
}

// RegisterDevPrinter enables the human-readable stdout printer without
// setting up an OTel trace exporter. Useful when you already have your
// own TracerProvider configured.
func RegisterDevPrinter() {
	devPrinter = defaultDevPrinter
}
