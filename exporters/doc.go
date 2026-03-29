// Package exporters provides OTel exporters for Otellix telemetry data.
//
// stdout.go implements a human-readable stdout exporter for local development.
// When enabled via otellix.SetupDev() or otellix.RegisterDevPrinter(), it prints
// a clean summary of every LLM call:
//
//	[otellix] anthropic/claude-sonnet-4-6 | feature: civic-search | user: usr_123
//	          tokens: 245 in + 891 out | cost: $0.000156 | latency: 1.2s
//	          prompt_fingerprint: a3f8b1c2 | budget: $0.041/$0.050 (82%)
package exporters
