package redis

import (
	"context"
	"testing"
	"time"

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
