// Package otellix provides OpenTelemetry-native LLM observability for Go.
package otellix

import "context"

// contextKey is a private type to avoid collisions in context.Context.
type contextKey string

const (
	userIDKey    contextKey = "otellix.user_id"
	projectIDKey contextKey = "otellix.project_id"
)

// ContextWithUser returns a new context with the given UserID.
func ContextWithUser(ctx context.Context, userID string) context.Context {
	return context.WithValue(ctx, userIDKey, userID)
}

// ContextWithProject returns a new context with the given ProjectID.
func ContextWithProject(ctx context.Context, projectID string) context.Context {
	return context.WithValue(ctx, projectIDKey, projectID)
}

// UserFromContext retrieves the UserID from the context, if present.
func UserFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

// ProjectFromContext retrieves the ProjectID from the context, if present.
func ProjectFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(projectIDKey).(string); ok {
		return v
	}
	return ""
}

// Config holds the configuration for a single traced LLM call.
type Config struct {
	// Provider identifies the LLM provider (e.g. "anthropic", "openai", "gemini", "ollama").
	Provider string

	// Model is the specific model identifier (e.g. "claude-sonnet-4-6", "gpt-4o").
	Model string

	// FeatureID identifies which product feature triggered this call (for cost attribution).
	FeatureID string

	// UserID identifies who triggered the call (for per-user cost attribution and budget enforcement).
	UserID string

	// ProjectID identifies which tenant or project this call belongs to (for multi-tenant billing).
	ProjectID string

	// SpanName overrides the default OTel span name ("llm.call").
	SpanName string

	// Attributes holds arbitrary key-value metadata attached to the OTel span.
	Attributes map[string]string

	// EnablePromptFingerprint enables SHA256 fingerprinting of the prompt for drift detection.
	EnablePromptFingerprint bool

	// PromptText is the raw prompt text used for fingerprinting (system prompt + first user message).
	PromptText string

	// FallbackModel is the cheaper model to switch to when FallbackDowngrade is triggered.
	FallbackModel string

	// BudgetConfig holds budget enforcement settings. Nil means no budget enforcement.
	BudgetConfig *BudgetConfig

	// PromptDecorator is a function that can modify prompt params based on budget context.
	PromptDecorator PromptDecorator

	// CacheConfig holds request deduplication configuration. Nil means no caching.
	CacheConfig *CacheConfig

	// AnomalyConfig holds cost anomaly detection configuration. Nil means no anomaly detection.
	AnomalyConfig *AnomalyConfig
}

// Option is a functional option for configuring a traced LLM call.
type Option func(*Config)

// WithProvider sets the LLM provider name.
func WithProvider(provider string) Option {
	return func(c *Config) { c.Provider = provider }
}

// WithModel sets the LLM model identifier.
func WithModel(model string) Option {
	return func(c *Config) { c.Model = model }
}

// WithFeatureID sets the product feature identifier for cost attribution.
func WithFeatureID(featureID string) Option {
	return func(c *Config) { c.FeatureID = featureID }
}

// WithUserID sets the user identifier for per-user cost tracking.
func WithUserID(userID string) Option {
	return func(c *Config) { c.UserID = userID }
}

// WithProjectID sets the project/tenant identifier.
func WithProjectID(projectID string) Option {
	return func(c *Config) { c.ProjectID = projectID }
}

// WithSpanName overrides the default span name.
func WithSpanName(name string) Option {
	return func(c *Config) { c.SpanName = name }
}

// WithAttributes sets arbitrary key-value metadata on the span.
func WithAttributes(attrs map[string]string) Option {
	return func(c *Config) { c.Attributes = attrs }
}

// WithPromptFingerprint enables prompt fingerprinting with the given prompt text.
func WithPromptFingerprint(promptText string) Option {
	return func(c *Config) {
		c.EnablePromptFingerprint = true
		c.PromptText = promptText
	}
}

// WithFallbackModel sets the cheaper model for budget downgrade fallback.
func WithFallbackModel(model string) Option {
	return func(c *Config) { c.FallbackModel = model }
}

// WithBudgetConfig sets budget enforcement configuration.
func WithBudgetConfig(bc *BudgetConfig) Option {
	return func(c *Config) { c.BudgetConfig = bc }
}

// WithPromptDecorator sets a function to dynamically decorate prompts based on budget.
func WithPromptDecorator(pd PromptDecorator) Option {
	return func(c *Config) { c.PromptDecorator = pd }
}

// WithRequestCache enables request deduplication with the given cache configuration.
func WithRequestCache(cfg *CacheConfig) Option {
	return func(c *Config) { c.CacheConfig = cfg }
}

// WithAnomalyDetection enables cost anomaly detection with the given configuration.
func WithAnomalyDetection(cfg *AnomalyConfig) Option {
	return func(c *Config) { c.AnomalyConfig = cfg }
}

// NewConfig creates a Config from functional options.
func NewConfig(opts ...Option) *Config {
	cfg := &Config{
		Attributes: make(map[string]string),
	}
	for _, opt := range opts {
		opt(cfg)
	}
	return cfg
}
