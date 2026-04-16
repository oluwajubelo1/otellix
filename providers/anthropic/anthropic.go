// Package anthropic provides an Otellix provider wrapper for the Anthropic API.
package anthropic

import (
	"context"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/oluwajubelo1/otellix/providers"

	anthropicsdk "github.com/anthropics/anthropic-sdk-go"
	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/anthropics/anthropic-sdk-go/packages/ssestream"
)

// Provider wraps the official Anthropic SDK client and implements providers.Provider.
type Provider struct {
	client anthropicsdk.Client
}

// New creates a new Anthropic provider. The API key is read from ANTHROPIC_API_KEY
// environment variable by default, or can be passed via options.
func New(opts ...option.RequestOption) *Provider {
	client := anthropicsdk.NewClient(opts...)
	return &Provider{client: client}
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "anthropic" }

// Call sends a message to the Anthropic API and returns a standardised CallResult.
func (p *Provider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	model := params.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	maxTokens := int64(params.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	// Build the messages for the API call.
	messages := make([]anthropicsdk.MessageParam, 0, len(params.Messages))
	for _, msg := range params.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, anthropicsdk.NewUserMessage(
				anthropicsdk.NewTextBlock(msg.Content),
			))
		case "assistant":
			messages = append(messages, anthropicsdk.NewAssistantMessage(
				anthropicsdk.NewTextBlock(msg.Content),
			))
		}
	}

	// Build the request parameters.
	reqParams := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	// Set system prompt if provided.
	if params.SystemPrompt != "" {
		reqParams.System = []anthropicsdk.TextBlockParam{
			{Text: params.SystemPrompt},
		}
	}

	// Set temperature if provided.
	if params.Temperature != nil {
		reqParams.Temperature = anthropicsdk.Float(*params.Temperature)
	}

	// Execute the API call.
	resp, err := p.client.Messages.New(ctx, reqParams)
	if err != nil {
		return providers.CallResult{}, classifyError(model, err)
	}

	// Map response to standardised CallResult.
	result := providers.CallResult{
		InputTokens:  resp.Usage.InputTokens,
		OutputTokens: resp.Usage.OutputTokens,
		Model:        string(resp.Model),
		RawResponse:  resp,
	}

	// Extract cached tokens (Anthropic prompt caching).
	result.CacheReadTokens = resp.Usage.CacheReadInputTokens
	result.CacheWriteTokens = resp.Usage.CacheCreationInputTokens

	return result, nil
}

// classifyError wraps raw errors with typed Otellix error types.
func classifyError(model string, err error) error {
	if err == nil {
		return nil
	}

	// Check for context deadline exceeded (timeout).
	if errors.Is(err, context.DeadlineExceeded) {
		return &providers.TimeoutError{
			Provider: "anthropic",
			Model:    model,
			Err:      err,
		}
	}

	// Check for context cancelled.
	if errors.Is(err, context.Canceled) {
		return &providers.TimeoutError{
			Provider: "anthropic",
			Model:    model,
			Err:      err,
		}
	}

	// Check for HTTP status-based errors — the Anthropic SDK returns an *apierror.Error.
	// We check for 429 rate limits via the error message as a pragmatic approach.
	var httpErr interface {
		StatusCode() int
	}
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode() == http.StatusTooManyRequests {
			return &providers.RateLimitError{
				Provider: "anthropic",
				Model:    model,
				Err:      err,
			}
		}
	}

	// Wrap all other errors with provider context.
	return &providers.ProviderError{
		Provider: "anthropic",
		Model:    model,
		Err:      fmt.Errorf("API call failed: %w", err),
	}
}

type anthropicStream struct {
	base *ssestream.Stream[anthropicsdk.MessageStreamEventUnion]
}

func (s *anthropicStream) Recv() (providers.StreamEvent, error) {
	if !s.base.Next() {
		err := s.base.Err()
		if err != nil {
			return providers.StreamEvent{}, err
		}
		return providers.StreamEvent{}, io.EOF
	}

	evt := s.base.Current()
	var res providers.StreamEvent

	switch evt.Type {
	case "message_start":
		res.InputTokens = evt.Message.Usage.InputTokens
	case "message_delta":
		res.OutputTokens = evt.Usage.OutputTokens
	case "content_block_delta":
		res.Token = evt.Delta.Text
	}

	return res, nil
}

func (s *anthropicStream) Close() error {
	return s.base.Close()
}

// Stream sends a request to the Anthropic API and streams the response back.
func (p *Provider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	model := params.Model
	if model == "" {
		model = "claude-sonnet-4-6"
	}

	maxTokens := int64(params.MaxTokens)
	if maxTokens == 0 {
		maxTokens = 1024
	}

	messages := make([]anthropicsdk.MessageParam, 0, len(params.Messages))
	for _, msg := range params.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, anthropicsdk.NewUserMessage(
				anthropicsdk.NewTextBlock(msg.Content),
			))
		case "assistant":
			messages = append(messages, anthropicsdk.NewAssistantMessage(
				anthropicsdk.NewTextBlock(msg.Content),
			))
		}
	}

	reqParams := anthropicsdk.MessageNewParams{
		Model:     anthropicsdk.Model(model),
		MaxTokens: maxTokens,
		Messages:  messages,
	}

	if params.SystemPrompt != "" {
		reqParams.System = []anthropicsdk.TextBlockParam{
			{Text: params.SystemPrompt},
		}
	}

	if params.Temperature != nil {
		reqParams.Temperature = anthropicsdk.Float(*params.Temperature)
	}

	stream := p.client.Messages.NewStreaming(ctx, reqParams)
	return &anthropicStream{base: stream}, nil
}
