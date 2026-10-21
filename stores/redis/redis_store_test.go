package redis

import (
	"context"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/redis/go-redis/v9"
)

// NOTE: This test requires a local Redis instance running on port 6379.
// You can start one with: docker-compose -f docker-compose.dev.yml up -d
func TestRedisBudgetStore(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	// Check if Redis is available, skip if not.
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration test")
	}

	interval := 5 * time.Second
	store := NewRedisBudgetStore(client, "test:otellix", interval)
	key := "user:test-123"

	// 1. Initial spend should be 0
	if spend := store.GetSpend(ctx, key); spend != 0 {
		t.Errorf("expected 0 spend, got %v", spend)
	}

	// 2. Add some spend
	store.AddSpend(ctx, key, 0.50)
	store.AddSpend(ctx, key, 0.25)

	if spend := store.GetSpend(ctx, key); spend != 0.75 {
		t.Errorf("expected 0.75 spend, got %v", spend)
	}

	// 3. Test rolling window (wait for expiry)
	t.Log("Waiting for window to slide...")
	time.Sleep(interval + 1*time.Second)

	if spend := store.GetSpend(ctx, key); spend != 0 {
		t.Errorf("expected 0 spend after expiry, got %v", spend)
	}

	// 4. Test Reset Time
	now := time.Now()
	store.AddSpend(ctx, key, 1.00)
	resetAt := store.GetResetTime(ctx, key)

	expectedReset := now.Add(interval)
	if resetAt.Sub(expectedReset).Abs() > 2*time.Second {
		t.Errorf("reset time %v is too far from expected %v", resetAt, expectedReset)
	}
}

func TestKeyTTLSeconds(t *testing.T) {
	tests := []struct {
		interval time.Duration
		want     int64
	}{
		{5 * time.Second, 3605},
		{24 * time.Hour, 90000},
		{30 * 24 * time.Hour, 2595600},
		{1500 * time.Millisecond, 3602},
	}
	for _, tt := range tests {
		if got := keyTTLSeconds(tt.interval); got != tt.want {
			t.Errorf("keyTTLSeconds(%v) = %d, want %d", tt.interval, got, tt.want)
		}
	}
}

// Regression test: keys used to expire after a fixed 48 hours, which wiped
// spend history for windows longer than that.
func TestRedisBudgetStoreLongWindowTTL(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration test")
	}

	interval := 30 * 24 * time.Hour
	store := NewRedisBudgetStore(client, "test:otellix:ttl", interval)
	key := "user:test-ttl"
	defer client.Del(ctx, "test:otellix:ttl:"+key)

	store.AddSpend(ctx, key, 0.10)

	ttl, err := client.TTL(ctx, "test:otellix:ttl:"+key).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl < interval {
		t.Errorf("key TTL %v is shorter than the %v window", ttl, interval)
	}
}

func TestRedisBudgetStoreCalendarMonth(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration test")
	}

	store := NewRedisBudgetStoreWithWindow(client, "test:otellix:month", otellix.CalendarMonth(time.UTC))
	key := "user:test-month"
	fullKey := "test:otellix:month:" + key
	client.Del(ctx, fullKey)
	defer client.Del(ctx, fullKey)

	// Spend from the last second of the previous month must not count.
	now := time.Now().UTC()
	monthStart := time.Date(now.Year(), now.Month(), 1, 0, 0, 0, 0, time.UTC)
	client.ZAdd(ctx, fullKey, redis.Z{Score: float64(monthStart.Unix() - 1), Member: "old:5.00"})

	store.AddSpend(ctx, key, 0.25)
	store.AddSpend(ctx, key, 0.50)

	if spend := store.GetSpend(ctx, key); spend != 0.75 {
		t.Errorf("expected 0.75 spend this month, got %v", spend)
	}

	nextMonth := monthStart.AddDate(0, 1, 0)
	if reset := store.GetResetTime(ctx, key); !reset.Equal(nextMonth) {
		t.Errorf("expected reset at %v, got %v", nextMonth, reset)
	}

	ttl, err := client.TTL(ctx, fullKey).Result()
	if err != nil {
		t.Fatalf("TTL: %v", err)
	}
	if ttl < 31*24*time.Hour {
		t.Errorf("key TTL %v is shorter than a month", ttl)
	}
}
