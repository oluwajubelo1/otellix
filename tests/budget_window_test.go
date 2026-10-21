package tests

import (
	"context"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
)

func TestBudgetWindowStartAndReset(t *testing.T) {
	lagos, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		t.Fatalf("load location: %v", err)
	}

	tests := []struct {
		name      string
		window    otellix.BudgetWindow
		now       time.Time
		wantStart time.Time
		wantReset time.Time
	}{
		{
			name:      "calendar month mid-month",
			window:    otellix.CalendarMonth(nil),
			now:       time.Date(2026, 10, 21, 15, 4, 5, 0, time.UTC),
			wantStart: time.Date(2026, 10, 1, 0, 0, 0, 0, time.UTC),
			wantReset: time.Date(2026, 11, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "calendar month rolls over the year",
			window:    otellix.CalendarMonth(time.UTC),
			now:       time.Date(2026, 12, 31, 23, 59, 59, 0, time.UTC),
			wantStart: time.Date(2026, 12, 1, 0, 0, 0, 0, time.UTC),
			wantReset: time.Date(2027, 1, 1, 0, 0, 0, 0, time.UTC),
		},
		{
			// 23:30 UTC on 31 Oct is already 1 Nov in Lagos (UTC+1).
			name:      "calendar month uses the window's location",
			window:    otellix.CalendarMonth(lagos),
			now:       time.Date(2026, 10, 31, 23, 30, 0, 0, time.UTC),
			wantStart: time.Date(2026, 11, 1, 0, 0, 0, 0, lagos),
			wantReset: time.Date(2026, 12, 1, 0, 0, 0, 0, lagos),
		},
		{
			name:      "calendar day",
			window:    otellix.CalendarDay(time.UTC),
			now:       time.Date(2026, 10, 21, 15, 4, 5, 0, time.UTC),
			wantStart: time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
			wantReset: time.Date(2026, 10, 22, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "rolling 30 days",
			window:    otellix.RollingWindow(30 * 24 * time.Hour),
			now:       time.Date(2026, 10, 21, 0, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 9, 21, 0, 0, 0, 0, time.UTC),
			wantReset: time.Date(2026, 11, 20, 0, 0, 0, 0, time.UTC),
		},
		{
			name:      "zero value is rolling 24 hours",
			window:    otellix.BudgetWindow{},
			now:       time.Date(2026, 10, 21, 12, 0, 0, 0, time.UTC),
			wantStart: time.Date(2026, 10, 20, 12, 0, 0, 0, time.UTC),
			wantReset: time.Date(2026, 10, 22, 12, 0, 0, 0, time.UTC),
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			if got := tt.window.Start(tt.now); !got.Equal(tt.wantStart) {
				t.Errorf("Start = %v, want %v", got, tt.wantStart)
			}
			if got := tt.window.ResetAt(tt.now, time.Time{}); !got.Equal(tt.wantReset) {
				t.Errorf("ResetAt = %v, want %v", got, tt.wantReset)
			}
			// Spend from the start of the window still counts at now, so stores must keep it that long.
			if age := tt.window.MaxAge(); age < tt.now.Sub(tt.wantStart) {
				t.Errorf("MaxAge = %v, shorter than %v", age, tt.now.Sub(tt.wantStart))
			}
		})
	}
}

func TestInMemoryStoreCalendarMonth(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStoreWithWindow(otellix.CalendarMonth(time.UTC))

	store.AddSpend(ctx, "user:monthly", 0.40)
	store.AddSpend(ctx, "user:monthly", 0.35)

	if spend := store.GetSpend(ctx, "user:monthly"); spend < 0.7499 || spend > 0.7501 {
		t.Errorf("expected spend 0.75, got %f", spend)
	}

	now := time.Now().UTC()
	nextMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	if reset := store.GetResetTime(ctx, "user:monthly"); !reset.Equal(nextMonth) {
		t.Errorf("expected reset at %v, got %v", nextMonth, reset)
	}
}

func TestInMemoryStoreRollingWindowMergedSpend(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(30 * 24 * time.Hour)

	for i := 0; i < 1000; i++ {
		store.AddSpend(ctx, "user:busy", 0.001)
	}
	if spend := store.GetSpend(ctx, "user:busy"); spend < 0.9999 || spend > 1.0001 {
		t.Errorf("expected spend 1.00, got %f", spend)
	}
}

func TestBudgetConfigWindowUsedForDefaultStore(t *testing.T) {
	ctx := context.Background()
	window := otellix.CalendarMonth(time.UTC)
	enforcer := otellix.NewBudgetEnforcer(&otellix.BudgetConfig{
		PerUserLimit:   1.00,
		FallbackAction: otellix.FallbackBlock,
		Window:         &window,
	})

	enforcer.Record(ctx, "user_monthly", "", 0.95)
	if allowed, _ := enforcer.Check(ctx, "user_monthly", "", 0.10); allowed {
		t.Fatal("expected call over the monthly limit to be blocked")
	}

	now := time.Now().UTC()
	nextMonth := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC).AddDate(0, 1, 0)
	if reset := enforcer.BudgetError("user_monthly", "").ResetAt; !reset.Equal(nextMonth) {
		t.Errorf("expected reset at %v, got %v", nextMonth, reset)
	}
}

func TestBudgetConfigLimitFieldPrecedence(t *testing.T) {
	ctx := context.Background()
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	store.AddSpend(ctx, "user:u", 0.50)

	tests := []struct {
		name    string
		config  otellix.BudgetConfig
		allowed bool
	}{
		{"new field", otellix.BudgetConfig{PerUserLimit: 0.40}, false},
		{"deprecated field still works", otellix.BudgetConfig{PerUserDailyLimit: 0.40}, false},                      //nolint:staticcheck // testing the deprecated field
		{"new field wins over deprecated", otellix.BudgetConfig{PerUserLimit: 1.00, PerUserDailyLimit: 0.40}, true}, //nolint:staticcheck // testing the deprecated field
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			cfg := tt.config
			cfg.Store = store
			allowed, _ := otellix.NewBudgetEnforcer(&cfg).Check(ctx, "u", "", 0.01)
			if allowed != tt.allowed {
				t.Errorf("allowed = %v, want %v", allowed, tt.allowed)
			}
		})
	}
}
