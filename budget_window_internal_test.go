package otellix

import (
	"context"
	"testing"
	"time"
)

func TestInMemoryStoreBucketsStayBounded(t *testing.T) {
	ctx := context.Background()
	tests := []struct {
		name   string
		window BudgetWindow
	}{
		{"rolling 30 days", RollingWindow(30 * 24 * time.Hour)},
		{"calendar month", CalendarMonth(time.UTC)},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			store := NewInMemoryBudgetStoreWithWindow(tt.window)
			for i := 0; i < 10000; i++ {
				store.AddSpend(ctx, "user:busy", 0.0001)
			}
			if n := len(store.getOrCreate("user:busy").buckets); n != 1 {
				t.Errorf("expected calls in quick succession to share 1 bucket, got %d", n)
			}
		})
	}
}
