// Package otellix provides OpenTelemetry-native LLM observability for Go.
package otellix

import (
	"context"
	"time"
)

// contextKey is a private type to avoid collisions in context.Context.
type contextKey string

const (
	userIDKey    contextKey = "otellix.user_id"
	projectIDKey contextKey = "otellix.project_id"
	agentKey     contextKey = "otellix.agent"
	stepKey      contextKey = "otellix.step"
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

// ContextWithAgent returns a new context tagged with an agent name (for agent
// frameworks like LangGraph built on LangChainGo).
func ContextWithAgent(ctx context.Context, agent string) context.Context {
	return context.WithValue(ctx, agentKey, agent)
}

// AgentFromContext retrieves the agent name from the context, if present.
func AgentFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(agentKey).(string); ok {
		return v
	}
	return ""
}

// ContextWithStep returns a new context tagged with an agent step number.
func ContextWithStep(ctx context.Context, step int) context.Context {
	return context.WithValue(ctx, stepKey, step)
}

// StepFromContext retrieves the agent step from the context, if present.
func StepFromContext(ctx context.Context) int {
	if v, ok := ctx.Value(stepKey).(int); ok {
		return v
	}
	return 0
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

	// RetryConfig holds automatic retry settings for provider calls. Nil means no retries.
	RetryConfig *RetryConfig

	// CaptureContent enables capturing prompt/response text on spans, with
	// optional redaction applied. Nil means no content capture.
	CaptureContent *CaptureConfig

	// Timeout bounds the entire traced LLM call (including retries and streaming).
	// If set, a deadline is derived from the caller's context before the provider
	// is invoked. Zero means no timeout — the caller owns cancellation.
	Timeout time.Duration
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

// WithRetryConfig enables automatic retries for provider calls.
func WithRetryConfig(cfg *RetryConfig) Option {
	return func(c *Config) { c.RetryConfig = cfg }
}

// WithCaptureContent enables capturing prompt/response content on spans with
// optional PII redaction.
func WithCaptureContent(cfg *CaptureConfig) Option {
	return func(c *Config) { c.CaptureContent = cfg }
}

// WithTimeout bounds the traced LLM call with a deadline derived from the
// caller's context. The provider call (and any retries/streaming) will fail
// fast with a context deadline exceeded error once the timeout elapses, and the
// span is still exported so the timeout is visible in traces.
//
// The caller's context is used as the parent, so spans keep their relationship
// to the incoming request:
//
//	ctx, cancel := context.WithTimeout(r.Context(), 60*time.Second)
//	defer cancel()
//	result, err := otellix.Trace(ctx, provider, params, otellix.WithTimeout(60*time.Second))
func WithTimeout(timeout time.Duration) Option {
	return func(c *Config) { c.Timeout = timeout }
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
