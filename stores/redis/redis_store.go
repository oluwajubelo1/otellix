package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/oluwajubelo1/otellix"
	"github.com/redis/go-redis/v9"
)

// RedisBudgetStore implements the otellix.BudgetStore interface using Redis Sorted Sets.
// It is designed for distributed environments where multiple application instances
// need to share the same budget limits.
type RedisBudgetStore struct {
	client *redis.Client
	prefix string
	window otellix.BudgetWindow
}

// NewRedisBudgetStore creates a new RedisBudgetStore.
// client: an initialized go-redis client.
// prefix: a prefix for all keys (e.g., "otellix:budget").
// interval: the rolling window duration (e.g., 24 * time.Hour).
func NewRedisBudgetStore(client *redis.Client, prefix string, interval time.Duration) *RedisBudgetStore {
	return NewRedisBudgetStoreWithWindow(client, prefix, otellix.RollingWindow(interval))
}

// NewRedisBudgetStoreWithWindow creates a new RedisBudgetStore with the given
// budget window, e.g. otellix.CalendarMonth(time.UTC).
func NewRedisBudgetStoreWithWindow(client *redis.Client, prefix string, window otellix.BudgetWindow) *RedisBudgetStore {
	return &RedisBudgetStore{
		client: client,
		prefix: prefix,
		window: window,
	}
}

// keyTTLMargin keeps a key alive slightly past its window so the oldest
// event is pruned by the window start rather than by key expiry.
const keyTTLMargin = time.Hour

// keyTTLSeconds returns how long a budget key should live after its latest write.
func keyTTLSeconds(interval time.Duration) int64 {
	return int64((interval + keyTTLMargin + time.Second - 1) / time.Second)
}

// Lua scripts to ensure atomicity and minimize network round-trips.
var addSpendScript = redis.NewScript(`
local key = KEYS[1]
local amount = ARGV[1]
local timestamp = ARGV[2]
local msg_id = ARGV[3]
local window_start = ARGV[4]
local ttl = ARGV[5]

-- Add the spend event to the Sorted Set.
-- Member format is "uuid:amount" to ensure uniqueness and simplify parsing.
redis.call('ZADD', key, timestamp, msg_id .. ':' .. amount)

-- Prune events from before the window start.
redis.call('ZREMRANGEBYSCORE', key, '-inf', '(' .. window_start)

-- Expire the whole key once every event in it is outside the window.
redis.call('EXPIRE', key, ttl)
return 1
`)

var getSpendScript = redis.NewScript(`
local key = KEYS[1]
local window_start = ARGV[1]

-- Prune events from before the window start.
redis.call('ZREMRANGEBYSCORE', key, '-inf', '(' .. window_start)

-- Get all valid members in the window.
local members = redis.call('ZRANGE', key, 0, -1)
local total = 0

for _, member in ipairs(members) do
    local colon_idx = member:find(":")
    if colon_idx then
        local amount_str = member:sub(colon_idx + 1)
        total = total + tonumber(amount_str)
    end
end
return tostring(total)
`)

// GetSpend returns the total spend for the key within the budget window.
func (s *RedisBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	fullKey := fmt.Sprintf("%s:%s", s.prefix, key)
	windowStart := s.window.Start(time.Now()).Unix()

	val, err := getSpendScript.Run(ctx, s.client, []string{fullKey}, windowStart).Result()
	if err != nil {
		return 0
	}

	f, _ := strconv.ParseFloat(val.(string), 64)
	return f
}

// AddSpend records a new spend amount for the key.
func (s *RedisBudgetStore) AddSpend(ctx context.Context, key string, amount float64) {
	fullKey := fmt.Sprintf("%s:%s", s.prefix, key)
	now := time.Now()
	timestamp := now.Unix()
	windowStart := s.window.Start(now).Unix()
	msgID := uuid.New().String()

	addSpendScript.Run(ctx, s.client, []string{fullKey}, amount, timestamp, msgID, windowStart, keyTTLSeconds(s.window.MaxAge()))
}

// GetResetTime returns when spend next drops: when the oldest event in a
// rolling window expires, or the start of the next calendar period.
func (s *RedisBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	fullKey := fmt.Sprintf("%s:%s", s.prefix, key)
	now := time.Now()

	// Get the oldest member in the window (lowest score).
	windowStart := strconv.FormatInt(s.window.Start(now).Unix(), 10)
	res, err := s.client.ZRangeByScoreWithScores(ctx, fullKey, &redis.ZRangeBy{
		Min: windowStart, Max: "+inf", Count: 1,
	}).Result()
	if err != nil || len(res) == 0 {
		return s.window.ResetAt(now, time.Time{})
	}

	return s.window.ResetAt(now, time.Unix(int64(res[0].Score), 0))
}

// Verify that RedisBudgetStore implements otellix.BudgetStore.
var _ otellix.BudgetStore = (*RedisBudgetStore)(nil)
