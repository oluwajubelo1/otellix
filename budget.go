package otellix

import (
	"context"
	"fmt"
	"sync"
	"time"
)

// FallbackAction determines what happens when a budget ceiling is exceeded.
type FallbackAction int

const (
	// FallbackBlock returns a BudgetExceededError immediately — the LLM is never called.
	FallbackBlock FallbackAction = iota

	// FallbackNotify calls the LLM but emits a budget.exceeded event on the span.
	FallbackNotify

	// FallbackDowngrade swaps to a cheaper model (Config.FallbackModel) and proceeds.
	FallbackDowngrade
)

// BudgetConfig holds budget enforcement settings.
type BudgetConfig struct {
	// PerUserDailyLimit is the maximum USD spend per user in the rolling window.
	PerUserDailyLimit float64

	// PerProjectDailyLimit is the maximum USD spend per project in the rolling window.
	PerProjectDailyLimit float64

	// FallbackAction determines what to do when the budget is exceeded.
	FallbackAction FallbackAction

	// FallbackModel is the cheaper model to use when FallbackDowngrade is triggered.
	FallbackModel string

	// ResetInterval is the rolling window duration. Default: 24 hours.
	ResetInterval time.Duration

	// Store is the backing store for budget data. Default: in-memory.
	Store BudgetStore
}

// BudgetStore is the interface for pluggable budget storage backends.
type BudgetStore interface {
	// GetSpend returns the current spend for a key within the rolling window.
	GetSpend(ctx context.Context, key string) float64

	// AddSpend records additional spend for a key.
	AddSpend(ctx context.Context, key string, amount float64)

	// GetResetTime returns when the current budget window resets for a key.
	GetResetTime(ctx context.Context, key string) time.Time
}

// BudgetExceededError is returned when a call is blocked by budget enforcement.
type BudgetExceededError struct {
	UserID       string
	ProjectID    string
	CurrentSpend float64
	Limit        float64
	ResetAt      time.Time
}

func (e *BudgetExceededError) Error() string {
	return fmt.Sprintf("budget exceeded: user=%s project=%s spend=$%.4f limit=$%.4f resets=%s",
		e.UserID, e.ProjectID, e.CurrentSpend, e.Limit, e.ResetAt.Format(time.RFC3339))
}



// spendEntry tracks spend with time-bucketed counters for rolling window expiry.
type spendEntry struct {
	mu      sync.Mutex
	buckets []spendBucket
}

type spendBucket struct {
	amount    float64
	timestamp time.Time
}

// InMemoryBudgetStore is a thread-safe, in-memory budget store using
// rolling time-bucketed counters. Suitable for single-process deployments.
type InMemoryBudgetStore struct {
	data     sync.Map // map[string]*spendEntry
	interval time.Duration
}

// NewInMemoryBudgetStore creates a new in-memory store with the given window.
func NewInMemoryBudgetStore(interval time.Duration) *InMemoryBudgetStore {
	return &InMemoryBudgetStore{interval: interval}
}

func (s *InMemoryBudgetStore) getOrCreate(key string) *spendEntry {
	val, _ := s.data.LoadOrStore(key, &spendEntry{})
	return val.(*spendEntry)
}

func (s *InMemoryBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	cutoff := time.Now().Add(-s.interval)
	var total float64
	// Sum only buckets within the rolling window.
	for _, b := range entry.buckets {
		if b.timestamp.After(cutoff) {
			total += b.amount
		}
	}
	return total
}

func (s *InMemoryBudgetStore) AddSpend(ctx context.Context, key string, amount float64) {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	// Append new bucket.
	entry.buckets = append(entry.buckets, spendBucket{
		amount:    amount,
		timestamp: time.Now(),
	})

	// Prune expired buckets to prevent unbounded growth.
	cutoff := time.Now().Add(-s.interval)
	pruned := entry.buckets[:0]
	for _, b := range entry.buckets {
		if b.timestamp.After(cutoff) {
			pruned = append(pruned, b)
		}
	}
	entry.buckets = pruned
}

func (s *InMemoryBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	if len(entry.buckets) == 0 {
		return time.Now().Add(s.interval)
	}
	// The oldest bucket in the window determines the next "reset" — when it expires.
	oldest := entry.buckets[0].timestamp
	return oldest.Add(s.interval)
}



// BudgetEnforcer checks and records budget usage.
type BudgetEnforcer struct {
	config *BudgetConfig
	store  BudgetStore
}

// NewBudgetEnforcer creates a new enforcer. Uses InMemoryBudgetStore if config.Store is nil.
func NewBudgetEnforcer(config *BudgetConfig) *BudgetEnforcer {
	interval := config.ResetInterval
	if interval == 0 {
		interval = 24 * time.Hour
	}
	if config.Store == nil {
		config.Store = NewInMemoryBudgetStore(interval)
	}
	return &BudgetEnforcer{config: config, store: config.Store}
}

// Check determines whether a call is allowed given current budget usage.
// Returns (allowed, fallbackAction).
func (e *BudgetEnforcer) Check(ctx context.Context, userID, projectID string, estimatedCost float64) (bool, FallbackAction) {
	// Check per-user limit.
	if e.config.PerUserDailyLimit > 0 && userID != "" {
		userSpend := e.store.GetSpend(ctx, "user:"+userID)
		if userSpend+estimatedCost > e.config.PerUserDailyLimit {
			return false, e.config.FallbackAction
		}
	}

	// Check per-project limit.
	if e.config.PerProjectDailyLimit > 0 && projectID != "" {
		projSpend := e.store.GetSpend(ctx, "project:"+projectID)
		if projSpend+estimatedCost > e.config.PerProjectDailyLimit {
			return false, e.config.FallbackAction
		}
	}

	return true, e.config.FallbackAction
}

// Record adds actual cost to the budget store after a successful call.
func (e *BudgetEnforcer) Record(ctx context.Context, userID, projectID string, actualCost float64) {
	if userID != "" {
		e.store.AddSpend(ctx, "user:"+userID, actualCost)
	}
	if projectID != "" {
		e.store.AddSpend(ctx, "project:"+projectID, actualCost)
	}
}

// Remaining returns the remaining budget for a user/project (whichever is lower).
func (e *BudgetEnforcer) Remaining(userID, projectID string) float64 {
	ctx := context.Background()
	remaining := float64(1<<63 - 1) // start at max

	if e.config.PerUserDailyLimit > 0 && userID != "" {
		userRemaining := e.config.PerUserDailyLimit - e.store.GetSpend(ctx, "user:"+userID)
		if userRemaining < remaining {
			remaining = userRemaining
		}
	}
	if e.config.PerProjectDailyLimit > 0 && projectID != "" {
		projRemaining := e.config.PerProjectDailyLimit - e.store.GetSpend(ctx, "project:"+projectID)
		if projRemaining < remaining {
			remaining = projRemaining
		}
	}
	return remaining
}

// Status returns the current detailed budget status for a user/project.
func (e *BudgetEnforcer) Status(ctx context.Context, userID, projectID string) BudgetStatus {
	if ctx == nil {
		ctx = context.Background()
	}

	var usage, limit float64
	var exceeded bool

	if userID != "" && e.config.PerUserDailyLimit > 0 {
		usage = e.store.GetSpend(ctx, "user:"+userID)
		limit = e.config.PerUserDailyLimit
	} else if projectID != "" && e.config.PerProjectDailyLimit > 0 {
		usage = e.store.GetSpend(ctx, "project:"+projectID)
		limit = e.config.PerProjectDailyLimit
	}

	if limit > 0 && usage >= limit {
		exceeded = true
	}

	return BudgetStatus{
		Usage:      usage,
		Remaining:  limit - usage,
		IsExceeded: exceeded,
		Mode:       e.config.FallbackAction,
	}
}

// BudgetError creates a BudgetExceededError with current state.
func (e *BudgetEnforcer) BudgetError(userID, projectID string) *BudgetExceededError {
	ctx := context.Background()
	var currentSpend, limit float64

	if userID != "" && e.config.PerUserDailyLimit > 0 {
		currentSpend = e.store.GetSpend(ctx, "user:"+userID)
		limit = e.config.PerUserDailyLimit
	} else if projectID != "" && e.config.PerProjectDailyLimit > 0 {
		currentSpend = e.store.GetSpend(ctx, "project:"+projectID)
		limit = e.config.PerProjectDailyLimit
	}

	resetKey := "user:" + userID
	if userID == "" {
		resetKey = "project:" + projectID
	}

	return &BudgetExceededError{
		UserID:       userID,
		ProjectID:    projectID,
		CurrentSpend: currentSpend,
		Limit:        limit,
		ResetAt:      e.store.GetResetTime(ctx, resetKey),
	}
}
