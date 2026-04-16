// Docker Compose demo app — makes mock LLM calls to demonstrate Otellix metrics.
// No API key required — uses a mock provider.
package main

import (
	"context"
	"fmt"
	"log"
	"math/rand"
	"os"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/exporters"
	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
)

func main() {
	// Set up Prometheus exporter.
	promExp, err := exporters.NewPrometheusExporter()
	if err != nil {
		log.Fatalf("failed to create prometheus exporter: %v", err)
	}
	otel.SetMeterProvider(promExp.MeterProvider())

	// Also register the dev printer for visibility.
	otellix.RegisterDevPrinter()

	// Serve metrics.
	port := os.Getenv("METRICS_PORT")
	if port == "" {
		port = "9090"
	}
	go func() {
		log.Printf("Serving metrics on :%s/metrics", port)
		if err := promExp.ListenAndServe(":" + port); err != nil {
			log.Fatalf("metrics server failed: %v", err)
		}
	}()

	// Budget config for the demo.
	budgetCfg := &otellix.BudgetConfig{
		PerUserDailyLimit:    5.00,
		PerProjectDailyLimit: 50.00,
		FallbackAction:       otellix.FallbackNotify,
		ResetInterval:        24 * time.Hour,
	}

	// Simulated features, users, and models.
	features := []string{"chat", "search", "summarize", "translate", "code-review"}
	users := []string{"usr_001", "usr_002", "usr_003", "usr_004", "usr_005"}
	models := []struct {
		provider string
		model    string
	}{
		{"anthropic", "claude-sonnet-4-6"},
		{"anthropic", "claude-haiku-4-5"},
		{"openai", "gpt-4o"},
		{"openai", "gpt-4o-mini"},
		{"gemini", "gemini-2.5-flash"},
	}

	log.Println("Starting Otellix demo — making mock LLM calls every 6 seconds...")

	// Make ~10 calls per minute with randomised parameters.
	ticker := time.NewTicker(6 * time.Second)
	defer ticker.Stop()

	for range ticker.C {
		m := models[rand.Intn(len(models))]
		feature := features[rand.Intn(len(features))]
		user := users[rand.Intn(len(users))]

		mock := &providers.MockProvider{
			ProviderName: m.provider,
			Result: providers.CallResult{
				InputTokens:     int64(100 + rand.Intn(2000)),
				OutputTokens:    int64(50 + rand.Intn(1500)),
				CacheReadTokens: int64(rand.Intn(200)),
				Model:           m.model,
			},
		}

		// Randomly simulate errors (~10% of calls).
		if rand.Float64() < 0.10 {
			mock.Err = &providers.ProviderError{
				Provider: m.provider,
				Model:    m.model,
				Err:      fmt.Errorf("simulated API error"),
			}
		}

		_, err := otellix.Trace(context.Background(), mock,
			providers.CallParams{
				Model:    m.model,
				Messages: []providers.Message{{Role: "user", Content: "Demo prompt"}},
			},
			otellix.WithProvider(m.provider),
			otellix.WithFeatureID(feature),
			otellix.WithUserID(user),
			otellix.WithProjectID("demo-project"),
			otellix.WithBudgetConfig(budgetCfg),
		)
		if err != nil {
			log.Printf("Call error (expected in demo): %v", err)
		}
	}
}
