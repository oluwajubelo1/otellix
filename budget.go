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
	// PerUserLimit is the maximum USD spend per user within the budget window.
	PerUserLimit float64

	// PerProjectLimit is the maximum USD spend per project within the budget window.
	PerProjectLimit float64

	// PerUserDailyLimit is the maximum USD spend per user within the budget window.
	// It is used only when PerUserLimit is zero.
	//
	// Deprecated: the limit applies to the configured window, which need not be
	// a day. Use PerUserLimit.
	PerUserDailyLimit float64

	// PerProjectDailyLimit is the maximum USD spend per project within the budget window.
	// It is used only when PerProjectLimit is zero.
	//
	// Deprecated: the limit applies to the configured window, which need not be
	// a day. Use PerProjectLimit.
	PerProjectDailyLimit float64

	// FallbackAction determines what to do when the budget is exceeded.
	FallbackAction FallbackAction

	// FallbackModel is the cheaper model to use when FallbackDowngrade is triggered.
	FallbackModel string

	// Window is the budget window for the default in-memory store, e.g.
	// CalendarMonth(time.UTC). It takes precedence over ResetInterval.
	// Ignored when Store is set: configure the store's window instead.
	Window *BudgetWindow

	// ResetInterval is the rolling window duration for the default in-memory
	// store. Default: 24 hours. Ignored when Store or Window is set.
	ResetInterval time.Duration

	// Store is the backing store for budget data. Default: in-memory.
	Store BudgetStore
}

// userLimit returns the per-user limit, falling back to the deprecated field.
func (c *BudgetConfig) userLimit() float64 {
	if c.PerUserLimit > 0 {
		return c.PerUserLimit
	}
	return c.PerUserDailyLimit
}

// projectLimit returns the per-project limit, falling back to the deprecated field.
func (c *BudgetConfig) projectLimit() float64 {
	if c.PerProjectLimit > 0 {
		return c.PerProjectLimit
	}
	return c.PerProjectDailyLimit
}

// BudgetStore is the interface for pluggable budget storage backends.
type BudgetStore interface {
	// GetSpend returns the current spend for a key within the budget window.
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

// spendEntry holds a key's spend as time-ordered buckets.
type spendEntry struct {
	mu      sync.Mutex
	buckets []spendBucket
}

// spendBucket is one or more spend events merged together. It counts
// towards the window until its latest event falls out of it.
type spendBucket struct {
	amount float64
	first  time.Time
	last   time.Time
}

// InMemoryBudgetStore is a thread-safe, in-memory budget store. Spend is
// lost when the process restarts, so it suits development, tests and
// single-process deployments. Use RedisBudgetStore for anything else.
type InMemoryBudgetStore struct {
	data   sync.Map // map[string]*spendEntry
	window BudgetWindow
}

// NewInMemoryBudgetStore creates a new in-memory store with a rolling window of the given length.
func NewInMemoryBudgetStore(interval time.Duration) *InMemoryBudgetStore {
	return NewInMemoryBudgetStoreWithWindow(RollingWindow(interval))
}

// NewInMemoryBudgetStoreWithWindow creates a new in-memory store with the given window,
// e.g. CalendarMonth(time.UTC).
func NewInMemoryBudgetStoreWithWindow(window BudgetWindow) *InMemoryBudgetStore {
	return &InMemoryBudgetStore{window: window}
}

func (s *InMemoryBudgetStore) getOrCreate(key string) *spendEntry {
	val, _ := s.data.LoadOrStore(key, &spendEntry{})
	return val.(*spendEntry)
}

// prune drops buckets that no longer count at now. Callers must hold entry.mu.
func (s *InMemoryBudgetStore) prune(entry *spendEntry, now time.Time) {
	start := s.window.Start(now)
	i := 0
	for i < len(entry.buckets) && entry.buckets[i].last.Before(start) {
		i++
	}
	entry.buckets = entry.buckets[i:]
}

func (s *InMemoryBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	s.prune(entry, time.Now())
	var total float64
	for _, b := range entry.buckets {
		total += b.amount
	}
	return total
}

func (s *InMemoryBudgetStore) AddSpend(ctx context.Context, key string, amount float64) {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now()
	s.prune(entry, now)

	if n := len(entry.buckets); n > 0 && s.window.canMerge(entry.buckets[n-1].first, now) {
		entry.buckets[n-1].amount += amount
		entry.buckets[n-1].last = now
		return
	}
	entry.buckets = append(entry.buckets, spendBucket{amount: amount, first: now, last: now})
}

func (s *InMemoryBudgetStore) GetResetTime(ctx context.Context, key string) time.Time {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	now := time.Now()
	s.prune(entry, now)
	var oldest time.Time
	if len(entry.buckets) > 0 {
		oldest = entry.buckets[0].last
	}
	return s.window.ResetAt(now, oldest)
}

// BudgetEnforcer checks and records budget usage.
type BudgetEnforcer struct {
	config *BudgetConfig
	store  BudgetStore
}

// NewBudgetEnforcer creates a new enforcer. Uses InMemoryBudgetStore if config.Store is nil.
func NewBudgetEnforcer(config *BudgetConfig) *BudgetEnforcer {
	if config.Store == nil {
		window := RollingWindow(config.ResetInterval)
		if config.Window != nil {
			window = *config.Window
		}
		config.Store = NewInMemoryBudgetStoreWithWindow(window)
	}
	return &BudgetEnforcer{config: config, store: config.Store}
}

// Check determines whether a call is allowed given current budget usage.
// Returns (allowed, fallbackAction).
func (e *BudgetEnforcer) Check(ctx context.Context, userID, projectID string, estimatedCost float64) (bool, FallbackAction) {
	// Check per-user limit.
	if e.config.userLimit() > 0 && userID != "" {
		userSpend := e.store.GetSpend(ctx, "user:"+userID)
		if userSpend+estimatedCost > e.config.userLimit() {
			return false, e.config.FallbackAction
		}
	}

	// Check per-project limit.
	if e.config.projectLimit() > 0 && projectID != "" {
		projSpend := e.store.GetSpend(ctx, "project:"+projectID)
		if projSpend+estimatedCost > e.config.projectLimit() {
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

	if e.config.userLimit() > 0 && userID != "" {
		userRemaining := e.config.userLimit() - e.store.GetSpend(ctx, "user:"+userID)
		if userRemaining < remaining {
			remaining = userRemaining
		}
	}
	if e.config.projectLimit() > 0 && projectID != "" {
		projRemaining := e.config.projectLimit() - e.store.GetSpend(ctx, "project:"+projectID)
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

	if userID != "" && e.config.userLimit() > 0 {
		usage = e.store.GetSpend(ctx, "user:"+userID)
		limit = e.config.userLimit()
	} else if projectID != "" && e.config.projectLimit() > 0 {
		usage = e.store.GetSpend(ctx, "project:"+projectID)
		limit = e.config.projectLimit()
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

	if userID != "" && e.config.userLimit() > 0 {
		currentSpend = e.store.GetSpend(ctx, "user:"+userID)
		limit = e.config.userLimit()
	} else if projectID != "" && e.config.projectLimit() > 0 {
		currentSpend = e.store.GetSpend(ctx, "project:"+projectID)
		limit = e.config.projectLimit()
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
