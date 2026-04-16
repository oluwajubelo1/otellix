package redis

import (
	"context"
	"fmt"
	"strconv"
	"time"

	"github.com/google/uuid"
	"github.com/redis/go-redis/v9"
	"github.com/oluwajubelo1/otellix"
)

// RedisBudgetStore implements the otellix.BudgetStore interface using Redis Sorted Sets.
// It is designed for distributed environments where multiple application instances
// need to share the same budget limits.
type RedisBudgetStore struct {
	client   *redis.Client
	prefix   string
	interval time.Duration
}

// NewRedisBudgetStore creates a new RedisBudgetStore.
// client: an initialized go-redis client.
// prefix: a prefix for all keys (e.g., "otellix:budget").
// interval: the rolling window duration (e.g., 24 * time.Hour).
func NewRedisBudgetStore(client *redis.Client, prefix string, interval time.Duration) *RedisBudgetStore {
	return &RedisBudgetStore{
		client:   client,
		prefix:   prefix,
		interval: interval,
	}
}

// Lua scripts to ensure atomicity and minimize network round-trips.
var addSpendScript = redis.NewScript(`
local key = KEYS[1]
local amount = ARGV[1]
local timestamp = ARGV[2]
local msg_id = ARGV[3]
local cutoff = ARGV[4]

-- Add the spend event to the Sorted Set.
-- Member format is "uuid:amount" to ensure uniqueness and simplify parsing.
redis.call('ZADD', key, timestamp, msg_id .. ':' .. amount)

-- Prune events outside the rolling window.
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

-- Set expiration for the whole key (sliding window cleanup).
redis.call('EXPIRE', key, 172800) -- 48 hours
return 1
`)

var getSpendScript = redis.NewScript(`
local key = KEYS[1]
local cutoff = ARGV[1]

-- Prune stale events first.
redis.call('ZREMRANGEBYSCORE', key, '-inf', cutoff)

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

// GetSpend returns the total spend for the key within the rolling window.
func (s *RedisBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	fullKey := fmt.Sprintf("%s:%s", s.prefix, key)
	cutoff := time.Now().Add(-s.interval).Unix()

	val, err := getSpendScript.Run(ctx, s.client, []string{fullKey}, cutoff).Result()
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
	cutoff := now.Add(-s.interval).Unix()
	msgID := uuid.New().String()

	addSpendScript.Run(ctx, s.client, []string{fullKey}, amount, timestamp, msgID, cutoff)
}

// GetResetTime returns when the oldest spend event in the current window will expire.
func (s *RedisBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	fullKey := fmt.Sprintf("%s:%s", s.prefix, key)
	
	// Get the oldest member in the set (lowest score).
	res, err := s.client.ZRANGEWithScores(ctx, fullKey, 0, 0).Result()
	if err != nil || len(res) == 0 {
		return time.Now().Add(s.interval)
	}
	
	oldest := time.Unix(int64(res[0].Score), 0)
	return oldest.Add(s.interval)
}

// Verify that RedisBudgetStore implements otellix.BudgetStore.
var _ otellix.BudgetStore = (*RedisBudgetStore)(nil)
