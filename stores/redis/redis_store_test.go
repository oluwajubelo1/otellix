package redis

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/redis/go-redis/v9"
	"go.opentelemetry.io/otel"
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

// TestRedisBudgetStoreReserve checks that concurrent reservations, as from
// several app instances, can't together hold more than a limit, and that a
// reservation must fit every limit it is checked against.
func TestRedisBudgetStoreReserve(t *testing.T) {
	client := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration test")
	}

	store := NewRedisBudgetStore(client, "test:otellix:reserve", time.Hour)
	userKey, projectKey := "user:test-reserve", "project:test-reserve"
	for _, k := range []string{userKey, projectKey} {
		client.Del(ctx, "test:otellix:reserve:"+k)
		defer client.Del(ctx, "test:otellix:reserve:"+k)
	}

	// Room for 3 holds of $0.10 within the $0.35 user limit.
	limits := []otellix.BudgetLimit{{Key: userKey, Limit: 0.35}, {Key: projectKey, Limit: 100}}
	var wg sync.WaitGroup
	var held atomic.Int32
	ids := make(chan string, 20)
	for range 20 {
		wg.Add(1)
		go func() {
			defer wg.Done()
			id, ok, err := store.Reserve(ctx, 0.10, limits)
			if err != nil {
				t.Errorf("Reserve: %v", err)
				return
			}
			if ok {
				held.Add(1)
				ids <- id
			}
		}()
	}
	wg.Wait()
	close(ids)

	if held.Load() != 3 {
		t.Fatalf("expected 3 reservations to fit, got %d", held.Load())
	}
	if spend := store.GetSpend(ctx, projectKey); spend < 0.2999 || spend > 0.3001 {
		t.Errorf("holds should count towards the project too: expected $0.30, got $%.4f", spend)
	}

	// The project limit is the tighter one here, so nothing is held anywhere.
	if _, ok, _ := store.Reserve(ctx, 0.01, []otellix.BudgetLimit{{Key: userKey, Limit: 100}, {Key: projectKey, Limit: 0.30}}); ok {
		t.Error("a reservation over the project limit should fail")
	}

	keys := []string{userKey, projectKey}
	for id := range ids {
		store.Release(ctx, id, keys)
	}
	for _, k := range keys {
		if spend := store.GetSpend(ctx, k); spend != 0 {
			t.Errorf("%s: expected no spend after releasing every hold, got $%.4f", k, spend)
		}
	}
}

// testClient returns a client for the local Redis, skipping the test if
// there isn't one, and deletes keys under prefix when the test ends.
func testClient(t *testing.T, prefix string) *redis.Client {
	t.Helper()
	client := redis.NewClient(&redis.Options{Addr: "localhost:6379"})
	ctx, cancel := context.WithTimeout(context.Background(), 2*time.Second)
	defer cancel()
	if err := client.Ping(ctx).Err(); err != nil {
		t.Skip("Redis not available on localhost:6379, skipping integration test")
	}
	clean := func() {
		keys, _ := client.Keys(context.Background(), prefix+":*").Result()
		if len(keys) > 0 {
			client.Del(context.Background(), keys...)
		}
	}
	clean()
	t.Cleanup(clean)
	return client
}

// captureErrors collects the errors reported through the OpenTelemetry
// error handler during the test.
func captureErrors(t *testing.T) func() []string {
	t.Helper()
	var mu sync.Mutex
	var errs []string
	previous := otel.GetErrorHandler()
	otel.SetErrorHandler(otel.ErrorHandlerFunc(func(err error) {
		mu.Lock()
		defer mu.Unlock()
		errs = append(errs, err.Error())
	}))
	t.Cleanup(func() { otel.SetErrorHandler(previous) })
	return func() []string {
		mu.Lock()
		defer mu.Unlock()
		return append([]string(nil), errs...)
	}
}

// TestRedisBudgetStoreMismatchedZonesKeepSpend checks that instances whose
// calendar days start at different times, sharing a prefix, don't delete
// spend the other still counts.
func TestRedisBudgetStoreMismatchedZonesKeepSpend(t *testing.T) {
	ctx := context.Background()
	const prefix = "test:otellix:zones"
	client := testClient(t, prefix)
	captureErrors(t) // the mismatch warning is expected here

	utcMinus12, err := time.LoadLocation("Etc/GMT+12")
	if err != nil {
		t.Skip("no time zone database")
	}
	utcDay, westDay := otellix.CalendarDay(time.UTC), otellix.CalendarDay(utcMinus12)
	utcStore := NewRedisBudgetStoreWithWindow(client, prefix, utcDay)
	westStore := NewRedisBudgetStoreWithWindow(client, prefix, westDay)

	// The two days start 12 hours apart. Put $1.00 between their starts,
	// so it counts for whichever day started first.
	now := time.Now()
	early, late := utcStore, westStore
	if westDay.Start(now).Before(utcDay.Start(now)) {
		early, late = westStore, utcStore
	}
	between := (utcDay.Start(now).Unix() + westDay.Start(now).Unix()) / 2
	key := "user:traveller"
	client.ZAdd(ctx, prefix+":"+key, redis.Z{Score: float64(between), Member: "e1:1.00"})

	if spend := early.GetSpend(ctx, key); spend != 1.00 {
		t.Fatalf("the earlier day should count the $1.00, got $%.2f", spend)
	}
	if spend := late.GetSpend(ctx, key); spend != 0 {
		t.Errorf("the later day shouldn't count spend from before it started, got $%.2f", spend)
	}
	if spend := early.GetSpend(ctx, key); spend != 1.00 {
		t.Errorf("reading through the other instance deleted spend this one counts: got $%.2f, want $1.00", spend)
	}
}

func TestRedisBudgetStoreWarnsOnWindowMismatch(t *testing.T) {
	ctx := context.Background()
	const prefix = "test:otellix:windows"
	client := testClient(t, prefix)
	errs := captureErrors(t)

	lagos, err := time.LoadLocation("Africa/Lagos")
	if err != nil {
		t.Skip("no time zone database")
	}
	NewRedisBudgetStoreWithWindow(client, prefix, otellix.CalendarDay(time.UTC)).GetSpend(ctx, "user:a")
	NewRedisBudgetStoreWithWindow(client, prefix, otellix.CalendarDay(time.UTC)).GetSpend(ctx, "user:b")
	if got := errs(); len(got) != 0 {
		t.Fatalf("instances with the same window shouldn't warn, got %q", got)
	}

	// Project budgets are a different namespace, so their window can differ.
	NewRedisBudgetStoreWithWindow(client, prefix, otellix.CalendarMonth(time.UTC)).GetSpend(ctx, "project:acme")
	if got := errs(); len(got) != 0 {
		t.Fatalf("a different window for project budgets shouldn't warn, got %q", got)
	}

	NewRedisBudgetStoreWithWindow(client, prefix, otellix.CalendarDay(lagos)).GetSpend(ctx, "user:c")
	got := errs()
	if len(got) != 1 || !strings.Contains(got[0], "calendar day in Africa/Lagos") || !strings.Contains(got[0], "calendar day in UTC") {
		t.Fatalf("expected one warning naming both windows, got %q", got)
	}
}

func TestRedisBudgetStoreWarnsOnLocalTime(t *testing.T) {
	const prefix = "test:otellix:local"
	client := testClient(t, prefix)
	errs := captureErrors(t)

	NewRedisBudgetStoreWithWindow(client, prefix, otellix.CalendarDay(time.Local))

	if got := errs(); len(got) != 1 || !strings.Contains(got[0], "named location") {
		t.Fatalf("expected a warning about time.Local, got %q", got)
	}
}
