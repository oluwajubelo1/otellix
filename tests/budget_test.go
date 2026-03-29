package tests

import (
	"context"
	"errors"
	"sync"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"

	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
)

func init() {
	// Set up a no-op tracer provider for tests.
	tp := sdktrace.NewTracerProvider()
	otel.SetTracerProvider(tp)
}

func TestBudgetUnderLimit(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	cfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 1.00, // $1.00 per user
		FallbackAction:    otellix.FallbackBlock,
		Store:             store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{
			InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4-6",
		},
	}

	result, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user1"),
		otellix.WithBudgetConfig(cfg),
	)
	if err != nil {
		t.Fatalf("expected call to succeed under budget: %v", err)
	}
	if result.InputTokens != 100 {
		t.Errorf("expected 100 input tokens, got %d", result.InputTokens)
	}
}

func TestBudgetPerUserBlock(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	// Pre-load spend so user is already over limit.
	store.AddSpend(context.Background(), "user:user_over", 0.06)

	cfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.05,
		FallbackAction:    otellix.FallbackBlock,
		Store:             store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4-6"},
	}

	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user_over"),
		otellix.WithBudgetConfig(cfg),
	)
	if err == nil {
		t.Fatal("expected BudgetExceededError, got nil")
	}

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected BudgetExceededError, got %T: %v", err, err)
	}
	if budgetErr.UserID != "user_over" {
		t.Errorf("expected user_id=user_over, got %s", budgetErr.UserID)
	}
	if mock.CallCount != 0 {
		t.Errorf("expected 0 provider calls when blocked, got %d", mock.CallCount)
	}
}

func TestBudgetPerProjectBlock(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(context.Background(), "project:proj_over", 3.00)

	cfg := &otellix.BudgetConfig{
		PerProjectDailyLimit: 2.00,
		FallbackAction:       otellix.FallbackBlock,
		Store:                store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4-6"},
	}

	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithProjectID("proj_over"),
		otellix.WithBudgetConfig(cfg),
	)
	if err == nil {
		t.Fatal("expected BudgetExceededError for project over limit")
	}

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected BudgetExceededError, got %T: %v", err, err)
	}
}

func TestBudgetFallbackDowngrade(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(context.Background(), "user:user_downgrade", 0.06)

	cfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.05,
		FallbackAction:    otellix.FallbackDowngrade,
		FallbackModel:     "claude-haiku-4-5",
		Store:             store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50},
	}

	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user_downgrade"),
		otellix.WithFallbackModel("claude-haiku-4-5"),
		otellix.WithBudgetConfig(cfg),
	)
	if err != nil {
		t.Fatalf("downgrade should not error: %v", err)
	}

	// The mock should have been called with the downgraded model.
	if mock.LastParams.Model != "claude-haiku-4-5" {
		t.Errorf("expected model to be downgraded to claude-haiku-4-5, got %s", mock.LastParams.Model)
	}
}

func TestBudgetFallbackNotify(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(context.Background(), "user:user_notify", 0.06)

	cfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.05,
		FallbackAction:    otellix.FallbackNotify,
		Store:             store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 100, OutputTokens: 50, Model: "claude-sonnet-4-6"},
	}

	_, err := otellix.Trace(context.Background(), mock,
		providers.CallParams{Model: "claude-sonnet-4-6"},
		otellix.WithProvider("anthropic"),
		otellix.WithUserID("user_notify"),
		otellix.WithBudgetConfig(cfg),
	)
	if err != nil {
		t.Fatalf("notify should not block: %v", err)
	}
	if mock.CallCount != 1 {
		t.Errorf("expected provider to be called once, got %d", mock.CallCount)
	}
}

func TestBudgetRollingWindow(t *testing.T) {
	// Use a very short window (100ms) so we can test expiry.
	store := otellix.NewInMemoryBudgetStore(100 * time.Millisecond)

	// Add spend.
	store.AddSpend(context.Background(), "user:user_rolling", 0.10)
	spend := store.GetSpend(context.Background(), "user:user_rolling")
	if spend != 0.10 {
		t.Fatalf("expected spend 0.10, got %f", spend)
	}

	// Wait for the window to expire.
	time.Sleep(150 * time.Millisecond)

	// Spend should be 0 now — the old buckets expired.
	spend = store.GetSpend(context.Background(), "user:user_rolling")
	if spend != 0 {
		t.Errorf("expected spend 0 after window expiry, got %f", spend)
	}
}

func TestBudgetConcurrentSafety(t *testing.T) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)

	cfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 100.00, // high limit so nothing gets blocked
		FallbackAction:    otellix.FallbackBlock,
		Store:             store,
	}

	mock := &providers.MockProvider{
		Result: providers.CallResult{InputTokens: 10, OutputTokens: 5, Model: "claude-sonnet-4-6"},
	}

	var wg sync.WaitGroup
	errs := make(chan error, 50)

	for i := 0; i < 50; i++ {
		wg.Add(1)
		go func(idx int) {
			defer wg.Done()
			_, err := otellix.Trace(context.Background(), mock,
				providers.CallParams{Model: "claude-sonnet-4-6"},
				otellix.WithProvider("anthropic"),
				otellix.WithUserID("concurrent_user"),
				otellix.WithBudgetConfig(cfg),
			)
			if err != nil {
				errs <- err
			}
		}(i)
	}

	wg.Wait()
	close(errs)

	for err := range errs {
		t.Errorf("unexpected error in concurrent test: %v", err)
	}
}
