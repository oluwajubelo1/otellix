package tests

import (
	"context"
	"errors"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// TestSeparateUserAndProjectStores checks that with a UserStore and a
// ProjectStore, each limit's spend goes to its own store and window.
func TestSeparateUserAndProjectStores(t *testing.T) {
	ctx := context.Background()
	userStore := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	projectStore := otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarMonth(time.UTC))
	budget := &otellix.BudgetConfig{
		PerUserLimit:    1,
		PerProjectLimit: 10,
		UserStore:       userStore,
		ProjectStore:    projectStore,
	}
	mock := &providers.MockProvider{ProviderName: "openai", Result: providers.CallResult{OutputTokens: 1000}}

	if _, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("ada"), otellix.WithProjectID("acme"), otellix.WithBudgetConfig(budget)); err != nil {
		t.Fatal(err)
	}

	cost := otellix.CalculateCost("openai", "gpt-4o", providers.CallResult{OutputTokens: 1000})
	for _, c := range []struct {
		name  string
		store *otellix.InMemoryBudgetStore
		key   string
		want  float64
	}{
		{"user spend in the user store", userStore, "user:ada", cost},
		{"project spend in the project store", projectStore, "project:acme", cost},
		{"no project spend in the user store", userStore, "project:acme", 0},
		{"no user spend in the project store", projectStore, "user:ada", 0},
	} {
		if got := c.store.GetSpend(ctx, c.key); got < c.want*0.999 || got > c.want*1.001 {
			t.Errorf("%s: got $%.6f, want $%.6f", c.name, got, c.want)
		}
	}
}

// TestSeparateStoresReleaseUserHoldWhenProjectIsFull checks that a call
// that fits the user's budget but not the project's holds nothing.
func TestSeparateStoresReleaseUserHoldWhenProjectIsFull(t *testing.T) {
	ctx := context.Background()
	userStore := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	projectStore := otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarMonth(time.UTC))
	budget := &otellix.BudgetConfig{
		PerUserLimit:    1,
		PerProjectLimit: 0.000001,
		FallbackAction:  otellix.FallbackBlock,
		UserStore:       userStore,
		ProjectStore:    projectStore,
	}
	mock := &providers.MockProvider{ProviderName: "openai"}

	_, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-4o"},
		otellix.WithUserID("ada"), otellix.WithProjectID("acme"), otellix.WithBudgetConfig(budget))

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected the project's limit to block the call, got %v", err)
	}
	if spend := userStore.GetSpend(ctx, "user:ada"); spend != 0 {
		t.Errorf("the user's hold should be released when the project's doesn't fit, got $%.6f held", spend)
	}
	// The project's calendar month resets on the 1st, midnight UTC.
	if want := otellix.CalendarMonth(time.UTC).ResetAt(time.Now(), time.Time{}); !budgetErr.ResetAt.Equal(want) {
		t.Errorf("expected the project's reset time %v, got %v", want, budgetErr.ResetAt)
	}
}
