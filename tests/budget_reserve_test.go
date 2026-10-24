package tests

import (
	"context"
	"errors"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// delayedProvider is a MockProvider whose calls take a while, so concurrent
// calls overlap.
type delayedProvider struct {
	providers.MockProvider
	delay time.Duration
}

func (p *delayedProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	time.Sleep(p.delay)
	return p.MockProvider.Call(ctx, params)
}

// TestConcurrentCallsCannotOvershootLimit is the race that reservations fix:
// calls made at the same time used to all read the same spend, all pass the
// check, and together overshoot the limit.
func TestConcurrentCallsCannotOvershootLimit(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	// Each call costs 1000 output tokens of gpt-4o, $0.01, and the estimate
	// matches it. The limit has room for 3 calls.
	budget := &otellix.BudgetConfig{
		PerUserLimit:          0.035,
		FallbackAction:        otellix.FallbackBlock,
		Store:                 store,
		EstimatedOutputTokens: 1000,
	}
	mock := &delayedProvider{
		MockProvider: providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{OutputTokens: 1000}},
		delay:        50 * time.Millisecond,
	}

	const callers = 20
	var wg sync.WaitGroup
	var mu sync.Mutex
	var allowed, blocked int
	for range callers {
		wg.Add(1)
		go func() {
			defer wg.Done()
			_, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-4o"},
				otellix.WithUserID("burst"),
				otellix.WithBudgetConfig(budget),
			)
			var budgetErr *otellix.BudgetExceededError
			mu.Lock()
			defer mu.Unlock()
			switch {
			case err == nil:
				allowed++
			case errors.As(err, &budgetErr):
				blocked++
			default:
				t.Errorf("unexpected error: %v", err)
			}
		}()
	}
	wg.Wait()

	if allowed != 3 || blocked != callers-3 {
		t.Errorf("expected 3 calls allowed and %d blocked, got %d allowed and %d blocked", callers-3, allowed, blocked)
	}
	if spend := store.GetSpend(ctx, "user:burst"); spend > budget.PerUserLimit {
		t.Errorf("spend $%.4f overshot the $%.4f limit", spend, budget.PerUserLimit)
	}
}

// TestReservationReleasedAfterCall checks that a call's hold is replaced by
// its actual cost when it succeeds, and dropped when it fails.
func TestReservationReleasedAfterCall(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	budget := &otellix.BudgetConfig{
		PerUserLimit:          1.00,
		FallbackAction:        otellix.FallbackBlock,
		Store:                 store,
		EstimatedOutputTokens: 10_000, // hold $0.10 while each call runs
	}
	params := providers.CallParams{Model: "gpt-4o"}

	failing := &providers.MockProvider{ProviderName: "openai", Err: errors.New("provider down")}
	if _, err := otellix.Trace(ctx, failing, params, otellix.WithUserID("u"), otellix.WithBudgetConfig(budget)); err == nil {
		t.Fatal("expected the provider error")
	}
	if spend := store.GetSpend(ctx, "user:u"); spend != 0 {
		t.Errorf("a failed call should leave no spend or hold, got $%.6f", spend)
	}

	ok := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{OutputTokens: 100}}
	if _, err := otellix.Trace(ctx, ok, params, otellix.WithUserID("u"), otellix.WithBudgetConfig(budget)); err != nil {
		t.Fatal(err)
	}
	want := otellix.CalculateCost("openai", "gpt-4o", providers.CallResult{OutputTokens: 100})
	if spend := store.GetSpend(ctx, "user:u"); spend < want*0.999 || spend > want*1.001 {
		t.Errorf("expected only the actual cost $%.6f, got $%.6f", want, spend)
	}
}

// TestEstimateUsesPromptAndMaxTokens checks that the pre-call estimate grows
// with the prompt and respects MaxTokens and ReserveWorstCase.
func TestEstimateUsesPromptAndMaxTokens(t *testing.T) {
	short := providers.CallParams{Model: "gpt-4o", Messages: []providers.Message{{Role: "user", Content: "hi"}}}
	long := providers.CallParams{Model: "gpt-4o", Messages: []providers.Message{{Role: "user", Content: strings.Repeat("word ", 40_000)}}}
	if s, l := otellix.EstimateCallCost("openai", "gpt-4o", short, 500), otellix.EstimateCallCost("openai", "gpt-4o", long, 500); l <= s*10 {
		t.Errorf("a 200k-character prompt should cost far more than %q: $%.6f vs $%.6f", "hi", l, s)
	}

	// A 50k-token prompt costs about $0.125 on gpt-4o, so a $0.05 limit
	// blocks it before the provider is called.
	mock := &providers.MockProvider{ProviderName: "openai"}
	budget := &otellix.BudgetConfig{PerUserLimit: 0.05, FallbackAction: otellix.FallbackBlock}
	_, err := otellix.Trace(context.Background(), mock, long, otellix.WithUserID("long-prompt"), otellix.WithBudgetConfig(budget))
	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) || mock.CallCount != 0 {
		t.Errorf("expected the long prompt to be blocked before the call, got err=%v calls=%d", err, mock.CallCount)
	}

	// 4000 output tokens of gpt-4o is $0.04. The default 500-token estimate
	// fits a $0.03 limit; the worst case of MaxTokens doesn't.
	withMax := providers.CallParams{Model: "gpt-4o", MaxTokens: 4000}
	for _, tc := range []struct {
		worstCase bool
		allowed   bool
	}{{worstCase: false, allowed: true}, {worstCase: true, allowed: false}} {
		budget := &otellix.BudgetConfig{PerUserLimit: 0.03, FallbackAction: otellix.FallbackBlock, ReserveWorstCase: tc.worstCase}
		_, err := otellix.Trace(context.Background(), mock, withMax, otellix.WithUserID("max-tokens"), otellix.WithBudgetConfig(budget))
		if allowed := err == nil; allowed != tc.allowed {
			t.Errorf("ReserveWorstCase=%v: expected allowed=%v, got err=%v", tc.worstCase, tc.allowed, err)
		}
	}
}

func TestUnpricedModelBlocked(t *testing.T) {
	mock := &providers.MockProvider{ProviderName: "openai"}
	budget := &otellix.BudgetConfig{PerUserLimit: 1.00, UnpricedModel: otellix.UnpricedBlock}

	_, err := otellix.Trace(context.Background(), mock, providers.CallParams{Model: "gpt-unreleased"},
		otellix.WithUserID("u"), otellix.WithBudgetConfig(budget))

	var unpriced *otellix.UnpricedModelError
	if !errors.As(err, &unpriced) || unpriced.Provider != "openai" || unpriced.Model != "gpt-unreleased" {
		t.Fatalf("expected an UnpricedModelError for openai/gpt-unreleased, got %v", err)
	}
	if mock.CallCount != 0 {
		t.Error("the provider should not be called")
	}
}

func TestUnpricedModelFallbackPriceCountsTowardsBudget(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	budget := &otellix.BudgetConfig{
		PerUserLimit:       1.00,
		Store:              store,
		UnpricedModel:      otellix.UnpricedBlock, // overridden by the fallback price
		UnpricedModelPrice: &otellix.PricingEntry{InputPricePerMToken: 10, OutputPricePerMToken: 30},
	}
	mock := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{InputTokens: 1000, OutputTokens: 1000}}

	if _, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-unreleased"},
		otellix.WithUserID("u"), otellix.WithBudgetConfig(budget)); err != nil {
		t.Fatal(err)
	}
	// 1000 × $10/M + 1000 × $30/M = $0.04
	if spend := store.GetSpend(ctx, "user:u"); spend < 0.0399 || spend > 0.0401 {
		t.Errorf("expected $0.04 of spend at the fallback price, got $%.6f", spend)
	}
}

func TestUnpricedModelAllowedAndFlagged(t *testing.T) {
	spans := recordSpans(t)
	mock := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{OutputTokens: 1000}}
	budget := &otellix.BudgetConfig{PerUserLimit: 1.00}

	if _, err := otellix.Trace(context.Background(), mock, providers.CallParams{Model: "gpt-unreleased"},
		otellix.WithUserID("u"), otellix.WithBudgetConfig(budget)); err != nil {
		t.Fatalf("the default policy should allow the call: %v", err)
	}
	if got := spanAttr(t, spans, "llm.pricing_missing"); got != "true" {
		t.Errorf("expected llm.pricing_missing=true on the span, got %q", got)
	}
}
