package otellix

import (
	"context"
	"fmt"
	"slices"
	"strconv"
	"sync"
	"sync/atomic"
	"time"

	"go.opentelemetry.io/otel"

	"github.com/oluwajubelo1/otellix/providers"
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

	// UnattributedLimit is the maximum USD spend, shared by all calls with no
	// user ID, within the budget window. Without it, calls with no user ID
	// skip per-user limits entirely.
	UnattributedLimit float64

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

	// EstimatedOutputTokens is how many output tokens the pre-call budget
	// check assumes a call will produce, capped at params.MaxTokens when that
	// is set. Default: 500. Input tokens are estimated from the prompt.
	EstimatedOutputTokens int64

	// ReserveWorstCase makes the pre-call check assume every call produces
	// its full params.MaxTokens of output. Concurrent calls then can never
	// overshoot a limit together, but a user can be blocked before reaching
	// it, because each call holds its worst case while it runs. Calls without
	// MaxTokens use EstimatedOutputTokens.
	ReserveWorstCase bool

	// UnpricedModel decides what happens when a call uses a model with no
	// pricing (see RegisterModel). Default: UnpricedAllow.
	UnpricedModel UnpricedModelAction

	// UnpricedModelPrice is the pricing used for models with no pricing of
	// their own, so their spend still counts towards budgets. It takes
	// precedence over UnpricedModel.
	UnpricedModelPrice *PricingEntry
}

// UnpricedModelAction determines what happens when a budgeted call uses a
// model with no pricing, whose cost would otherwise count as $0.
type UnpricedModelAction int

const (
	// UnpricedAllow lets the call go ahead. Its cost counts as $0, and it is
	// flagged with the llm.pricing_missing span attribute and the
	// otellix.llm.calls.unpriced metric.
	UnpricedAllow UnpricedModelAction = iota

	// UnpricedBlock returns an UnpricedModelError and never calls the provider.
	UnpricedBlock
)

// UnpricedModelError is returned when BudgetConfig.UnpricedModel is
// UnpricedBlock and a call uses a model with no pricing.
type UnpricedModelError struct {
	Provider string
	Model    string
}

func (e *UnpricedModelError) Error() string {
	return fmt.Sprintf("otellix: no pricing for %s/%s, so its cost can't be checked against the budget; add it with otellix.RegisterModel",
		e.Provider, e.Model)
}

// defaultEstimatedOutputTokens is the output assumed by the pre-call budget
// check when BudgetConfig.EstimatedOutputTokens is unset.
const defaultEstimatedOutputTokens = 500

// estimatedOutputTokens returns the output tokens the pre-call check assumes for params.
func (c *BudgetConfig) estimatedOutputTokens(params providers.CallParams) int64 {
	maxTokens := int64(params.MaxTokens)
	if c.ReserveWorstCase && maxTokens > 0 {
		return maxTokens
	}
	n := c.EstimatedOutputTokens
	if n <= 0 {
		n = defaultEstimatedOutputTokens
	}
	if maxTokens > 0 && maxTokens < n {
		return maxTokens
	}
	return n
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

// BudgetLimit is one limit a reservation must fit within.
type BudgetLimit struct {
	Key   string  // budget store key, e.g. "user:usr_123"
	Limit float64 // USD
}

// BudgetReserver is implemented by budget stores that can check limits and
// hold spend in one atomic step. Without it, concurrent calls can all pass
// the budget check before any of them records its spend, and together
// overshoot the limit. InMemoryBudgetStore and the Redis store implement it.
type BudgetReserver interface {
	// Reserve holds amount against every key in limits, but only if each
	// key's spend plus amount stays within its limit; otherwise it holds
	// nothing and returns ok=false. A hold counts as spend until it is
	// released or falls out of the budget window.
	Reserve(ctx context.Context, amount float64, limits []BudgetLimit) (id string, ok bool, err error)

	// Release drops the hold id from keys. Actual spend is recorded
	// separately with AddSpend.
	Release(ctx context.Context, id string, keys []string)
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

// spendEntry holds a key's spend as time-ordered buckets, plus the holds of
// calls still in flight.
type spendEntry struct {
	mu      sync.Mutex
	buckets []spendBucket
	holds   map[string]spendHold
}

// spendHold is spend reserved by a call in flight.
type spendHold struct {
	amount float64
	at     time.Time
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
	holdID atomic.Uint64
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

// prune drops buckets and holds that no longer count at now. Callers must hold entry.mu.
func (s *InMemoryBudgetStore) prune(entry *spendEntry, now time.Time) {
	start := s.window.Start(now)
	i := 0
	for i < len(entry.buckets) && entry.buckets[i].last.Before(start) {
		i++
	}
	entry.buckets = entry.buckets[i:]

	for id, h := range entry.holds {
		if h.at.Before(start) {
			delete(entry.holds, id)
		}
	}
}

// total returns the spend that counts at now, holds included. Callers must hold entry.mu.
func (s *InMemoryBudgetStore) total(entry *spendEntry, now time.Time) float64 {
	s.prune(entry, now)
	var total float64
	for _, b := range entry.buckets {
		total += b.amount
	}
	for _, h := range entry.holds {
		total += h.amount
	}
	return total
}

func (s *InMemoryBudgetStore) GetSpend(ctx context.Context, key string) float64 {
	entry := s.getOrCreate(key)
	entry.mu.Lock()
	defer entry.mu.Unlock()

	return s.total(entry, time.Now())
}

// Reserve holds amount against every key in limits if each key's spend plus
// amount stays within its limit.
func (s *InMemoryBudgetStore) Reserve(ctx context.Context, amount float64, limits []BudgetLimit) (string, bool, error) {
	keys := make([]string, len(limits))
	for i, l := range limits {
		keys[i] = l.Key
	}
	// Lock in sorted order so calls locking overlapping keys can't deadlock.
	slices.Sort(keys)
	keys = slices.Compact(keys)
	entries := make(map[string]*spendEntry, len(keys))
	for _, k := range keys {
		entry := s.getOrCreate(k)
		entry.mu.Lock()
		defer entry.mu.Unlock()
		entries[k] = entry
	}

	now := time.Now()
	for _, l := range limits {
		if s.total(entries[l.Key], now)+amount > l.Limit {
			return "", false, nil
		}
	}

	id := strconv.FormatUint(s.holdID.Add(1), 10)
	for _, entry := range entries {
		if entry.holds == nil {
			entry.holds = make(map[string]spendHold)
		}
		entry.holds[id] = spendHold{amount: amount, at: now}
	}
	return id, true, nil
}

// Release drops the hold id from keys.
func (s *InMemoryBudgetStore) Release(ctx context.Context, id string, keys []string) {
	for _, k := range keys {
		entry := s.getOrCreate(k)
		entry.mu.Lock()
		delete(entry.holds, id)
		entry.mu.Unlock()
	}
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

// unattributedKey is the budget store key shared by calls with no user ID.
const unattributedKey = "unattributed"

// BudgetEnforcer checks and records budget usage.
type BudgetEnforcer struct {
	config *BudgetConfig
	store  BudgetStore
}

// defaultStoreMu guards the lazy creation of a config's default store, so
// concurrent first calls on the same config share one store.
var defaultStoreMu sync.Mutex

// NewBudgetEnforcer creates a new enforcer. Uses InMemoryBudgetStore if config.Store is nil.
func NewBudgetEnforcer(config *BudgetConfig) *BudgetEnforcer {
	defaultStoreMu.Lock()
	defer defaultStoreMu.Unlock()

	if config.Store == nil {
		window := RollingWindow(config.ResetInterval)
		if config.Window != nil {
			window = *config.Window
		}
		config.Store = NewInMemoryBudgetStoreWithWindow(window)
	}
	return &BudgetEnforcer{config: config, store: config.Store}
}

// userBudget returns the store key and limit that apply to userID: the
// user's own budget, or the shared unattributed budget when userID is empty.
// An empty key means no user-level limit applies.
func (e *BudgetEnforcer) userBudget(userID string) (key string, limit float64) {
	if userID != "" {
		if l := e.config.userLimit(); l > 0 {
			return "user:" + userID, l
		}
		return "", 0
	}
	if e.config.UnattributedLimit > 0 {
		return unattributedKey, e.config.UnattributedLimit
	}
	return "", 0
}

// limits returns the limits that apply to a call: the user's (or the shared
// unattributed one) and the project's.
func (e *BudgetEnforcer) limits(userID, projectID string) []BudgetLimit {
	var limits []BudgetLimit
	if key, limit := e.userBudget(userID); key != "" {
		limits = append(limits, BudgetLimit{Key: key, Limit: limit})
	}
	if l := e.config.projectLimit(); l > 0 && projectID != "" {
		limits = append(limits, BudgetLimit{Key: "project:" + projectID, Limit: l})
	}
	return limits
}

// Check determines whether a call is allowed given current budget usage.
// Returns (allowed, fallbackAction).
//
// Check holds nothing, so calls checked at the same time can all be allowed
// and together overshoot a limit. Use Reserve to prevent that.
func (e *BudgetEnforcer) Check(ctx context.Context, userID, projectID string, estimatedCost float64) (bool, FallbackAction) {
	for _, l := range e.limits(userID, projectID) {
		if e.store.GetSpend(ctx, l.Key)+estimatedCost > l.Limit {
			return false, e.config.FallbackAction
		}
	}
	return true, e.config.FallbackAction
}

// Reservation is spend held against a call's budgets while the call runs,
// so calls made at the same time can't all pass the budget check at once.
type Reservation struct {
	store  BudgetReserver
	id     string
	keys   []string
	amount float64
	once   sync.Once
}

// Release drops the hold. It is safe to call more than once, and on a nil
// Reservation.
func (r *Reservation) Release(ctx context.Context) {
	if r == nil {
		return
	}
	r.once.Do(func() {
		// Release even if ctx was cancelled, or the hold would count against
		// the budget until it falls out of the window.
		r.store.Release(context.WithoutCancel(ctx), r.id, r.keys)
	})
}

// held returns the amount held, or 0 for a nil Reservation.
func (r *Reservation) held() float64 {
	if r == nil {
		return 0
	}
	return r.amount
}

// Reserve checks whether estimatedCost fits within every budget that applies
// to the call and, if it does, holds it until the returned Reservation is
// released. Record the call's actual cost with Record, then release the hold.
//
// It returns a nil Reservation when nothing was held: no limit applies, the
// call isn't allowed, or the store doesn't implement BudgetReserver, in which
// case it falls back to Check.
func (e *BudgetEnforcer) Reserve(ctx context.Context, userID, projectID string, estimatedCost float64) (*Reservation, bool) {
	limits := e.limits(userID, projectID)
	if len(limits) == 0 {
		return nil, true
	}
	reserver, ok := e.store.(BudgetReserver)
	if !ok {
		allowed, _ := e.Check(ctx, userID, projectID, estimatedCost)
		return nil, allowed
	}

	id, ok, err := reserver.Reserve(ctx, estimatedCost, limits)
	if err != nil {
		// Fail open, as GetSpend does when the store is unreachable: a budget
		// store outage shouldn't stop every LLM call.
		otel.Handle(fmt.Errorf("otellix: budget reservation failed, allowing the call: %w", err))
		return nil, true
	}
	if !ok {
		return nil, false
	}

	keys := make([]string, len(limits))
	for i, l := range limits {
		keys[i] = l.Key
	}
	return &Reservation{store: reserver, id: id, keys: keys, amount: estimatedCost}, true
}

// Record adds actual cost to the budget store after a successful call.
func (e *BudgetEnforcer) Record(ctx context.Context, userID, projectID string, actualCost float64) {
	if userID != "" {
		e.store.AddSpend(ctx, "user:"+userID, actualCost)
	} else if e.config.UnattributedLimit > 0 {
		e.store.AddSpend(ctx, unattributedKey, actualCost)
	}
	if projectID != "" {
		e.store.AddSpend(ctx, "project:"+projectID, actualCost)
	}
}

// Remaining returns the remaining budget for a user/project (whichever is lower).
func (e *BudgetEnforcer) Remaining(userID, projectID string) float64 {
	ctx := context.Background()
	remaining := float64(1<<63 - 1) // start at max

	if key, limit := e.userBudget(userID); key != "" {
		userRemaining := limit - e.store.GetSpend(ctx, key)
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

// tightestBudget returns the applicable budget with the least remaining:
// the user's (or the shared unattributed one) or the project's. An empty
// key means no limit applies.
func (e *BudgetEnforcer) tightestBudget(ctx context.Context, userID, projectID string) (key string, usage, limit float64) {
	if k, l := e.userBudget(userID); k != "" {
		key, limit = k, l
		usage = e.store.GetSpend(ctx, k)
	}
	if l := e.config.projectLimit(); l > 0 && projectID != "" {
		k := "project:" + projectID
		u := e.store.GetSpend(ctx, k)
		if key == "" || l-u < limit-usage {
			key, usage, limit = k, u, l
		}
	}
	return key, usage, limit
}

// Status returns the current detailed budget status for a user/project.
func (e *BudgetEnforcer) Status(ctx context.Context, userID, projectID string) BudgetStatus {
	if ctx == nil {
		ctx = context.Background()
	}

	_, usage, limit := e.tightestBudget(ctx, userID, projectID)

	return BudgetStatus{
		Usage:      usage,
		Remaining:  limit - usage,
		IsExceeded: limit > 0 && usage >= limit,
		Mode:       e.config.FallbackAction,
	}
}

// BudgetError creates a BudgetExceededError describing the tightest budget.
func (e *BudgetEnforcer) BudgetError(userID, projectID string) *BudgetExceededError {
	ctx := context.Background()
	key, currentSpend, limit := e.tightestBudget(ctx, userID, projectID)

	var resetAt time.Time
	if key != "" {
		resetAt = e.store.GetResetTime(ctx, key)
	}

	return &BudgetExceededError{
		UserID:       userID,
		ProjectID:    projectID,
		CurrentSpend: currentSpend,
		Limit:        limit,
		ResetAt:      resetAt,
	}
}
