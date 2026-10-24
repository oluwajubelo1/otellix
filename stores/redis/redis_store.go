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

// reserveScript holds an amount against several keys if every one of them
// has room for it. KEYS are the budget keys, and ARGV[6] onwards their limits.
var reserveScript = redis.NewScript(`
local amount = tonumber(ARGV[1])
local timestamp = ARGV[2]
local member = ARGV[3]
local window_start = ARGV[4]
local ttl = ARGV[5]

-- Check every limit before holding anything, so a call holds against all
-- of its budgets or none of them.
for i, key in ipairs(KEYS) do
    redis.call('ZREMRANGEBYSCORE', key, '-inf', '(' .. window_start)
    local total = 0
    for _, m in ipairs(redis.call('ZRANGE', key, 0, -1)) do
        local colon_idx = m:find(":")
        if colon_idx then
            total = total + tonumber(m:sub(colon_idx + 1))
        end
    end
    if total + amount > tonumber(ARGV[5 + i]) then
        return 0
    end
end

-- A hold is a spend event like any other, so it counts towards GetSpend
-- and falls out of the window if it is never released.
for _, key in ipairs(KEYS) do
    redis.call('ZADD', key, timestamp, member)
    redis.call('EXPIRE', key, ttl)
end
return 1
`)

// fullKey returns the Redis key for a budget key.
func (s *RedisBudgetStore) fullKey(key string) string {
	return fmt.Sprintf("%s:%s", s.prefix, key)
}

// GetSpend returns the total spend for the key within the budget window.
func (s *RedisBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	fullKey := s.fullKey(key)
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
	fullKey := s.fullKey(key)
	now := time.Now()
	timestamp := now.Unix()
	windowStart := s.window.Start(now).Unix()
	msgID := uuid.New().String()

	addSpendScript.Run(ctx, s.client, []string{fullKey}, amount, timestamp, msgID, windowStart, keyTTLSeconds(s.window.MaxAge()))
}

// GetResetTime returns when spend next drops: when the oldest event in a
// rolling window expires, or the start of the next calendar period.
func (s *RedisBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	fullKey := s.fullKey(key)
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

// Reserve holds amount against every key in limits if each key's spend plus
// amount stays within its limit. The check and the hold run in one Lua
// script, so calls from every instance sharing this Redis are serialised.
func (s *RedisBudgetStore) Reserve(ctx context.Context, amount float64, limits []otellix.BudgetLimit) (string, bool, error) {
	now := time.Now()
	member := fmt.Sprintf("hold-%s:%s", uuid.New().String(), strconv.FormatFloat(amount, 'f', -1, 64))

	keys := make([]string, len(limits))
	args := []any{amount, now.Unix(), member, s.window.Start(now).Unix(), keyTTLSeconds(s.window.MaxAge())}
	for i, l := range limits {
		keys[i] = s.fullKey(l.Key)
		args = append(args, l.Limit)
	}

	held, err := reserveScript.Run(ctx, s.client, keys, args...).Int()
	if err != nil {
		return "", false, err
	}
	return member, held == 1, nil
}

// Release drops the hold id from keys.
func (s *RedisBudgetStore) Release(ctx context.Context, id string, keys []string) {
	pipe := s.client.Pipeline()
	for _, k := range keys {
		pipe.ZRem(ctx, s.fullKey(k), id)
	}
	_, _ = pipe.Exec(ctx)
}

// Verify that RedisBudgetStore implements otellix.BudgetStore and otellix.BudgetReserver.
var (
	_ otellix.BudgetStore    = (*RedisBudgetStore)(nil)
	_ otellix.BudgetReserver = (*RedisBudgetStore)(nil)
)
