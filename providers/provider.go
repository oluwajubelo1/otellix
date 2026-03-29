// Package providers defines the interface that all LLM provider wrappers implement.
package providers

import (
	"context"
	"fmt"
	"sync"
)

// CallResult holds the standardized response from any LLM provider call.
type CallResult struct {
	// InputTokens is the number of tokens in the prompt/input.
	InputTokens int64

	// OutputTokens is the number of tokens in the completion/output.
	OutputTokens int64

	// CachedTokens is the number of tokens served from cache (Anthropic prompt caching, etc.).
	CachedTokens int64

	// Model is the actual model used (may differ from requested if provider auto-selects).
	Model string

	// RawResponse holds the provider's raw response for caller inspection.
	RawResponse interface{}
}

// CallParams holds the parameters for an LLM provider call.
type CallParams struct {
	// Model is the model identifier to use.
	Model string

	// Messages is the conversation messages to send.
	Messages []Message

	// MaxTokens is the maximum number of tokens to generate.
	MaxTokens int

	// Temperature controls randomness (0.0–1.0). Nil means use provider default.
	Temperature *float64

	// SystemPrompt is the system-level instruction.
	SystemPrompt string

	// Extra holds provider-specific parameters not covered by the standard fields.
	Extra map[string]interface{}
}

// Message represents a single message in a conversation.
type Message struct {
	Role    string // "user", "assistant", "system"
	Content string
}

// Provider is the interface that every LLM provider wrapper must implement.
// It abstracts the differences between providers into a single Call method.
type Provider interface {
	// Call sends a request to the LLM and returns a standardized result.
	Call(ctx context.Context, params CallParams) (CallResult, error)

	// Name returns the provider identifier (e.g. "anthropic", "openai").
	Name() string
}

// --- Error types ---

// RateLimitError is returned when the provider responds with HTTP 429.
type RateLimitError struct {
	Provider   string
	Model      string
	RetryAfter string // value of Retry-After header, if present
	Err        error
}

func (e *RateLimitError) Error() string {
	msg := fmt.Sprintf("%s/%s: rate limit exceeded", e.Provider, e.Model)
	if e.RetryAfter != "" {
		msg += fmt.Sprintf(" (retry after %s)", e.RetryAfter)
	}
	return msg
}

func (e *RateLimitError) Unwrap() error { return e.Err }

// TimeoutError is returned when the call exceeds the context deadline.
type TimeoutError struct {
	Provider string
	Model    string
	Err      error
}

func (e *TimeoutError) Error() string {
	return fmt.Sprintf("%s/%s: request timed out", e.Provider, e.Model)
}

func (e *TimeoutError) Unwrap() error { return e.Err }

// ProviderError is a general error from a provider call, wrapping the original error
// with provider and model context.
type ProviderError struct {
	Provider string
	Model    string
	Err      error
}

func (e *ProviderError) Error() string {
	return fmt.Sprintf("%s/%s: %v", e.Provider, e.Model, e.Err)
}

func (e *ProviderError) Unwrap() error { return e.Err }

// --- Mock provider for testing ---

// MockProvider is a test double that returns preconfigured results.
type MockProvider struct {
	mu           sync.Mutex
	ProviderName string
	Result       CallResult
	Err          error
	CallCount    int
	LastParams   CallParams
}

func (m *MockProvider) Call(ctx context.Context, params CallParams) (CallResult, error) {
	m.mu.Lock()
	defer m.mu.Unlock()
	m.CallCount++
	m.LastParams = params
	if m.Err != nil {
		return CallResult{}, m.Err
	}
	result := m.Result
	if result.Model == "" {
		result.Model = params.Model
	}
	return result, nil
}

func (m *MockProvider) Name() string {
	if m.ProviderName != "" {
		return m.ProviderName
	}
	return "mock"
}
