// Package openai provides an Otellix provider wrapper for the OpenAI API.
package openai

import (
	"context"
	"errors"
	"fmt"
	"net/http"

	"github.com/oluwajubelo1/otellix/providers"

	openaisdk "github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/packages/ssestream"
)

// Provider wraps the official OpenAI Go SDK and implements providers.Provider.
type Provider struct {
	client openaisdk.Client
}

// New creates a new OpenAI provider. The API key is read from OPENAI_API_KEY
// environment variable by default, or can be passed via options.
func New(opts ...option.RequestOption) *Provider {
	client := openaisdk.NewClient(opts...)
	return &Provider{client: client}
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "openai" }

// Call sends a chat completion request to the OpenAI API and returns a standardised CallResult.
func (p *Provider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	model := params.Model
	if model == "" {
		model = "gpt-4o"
	}

	// Build messages.
	messages := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(params.Messages)+1)

	// Add system prompt if provided.
	if params.SystemPrompt != "" {
		messages = append(messages, openaisdk.SystemMessage(params.SystemPrompt))
	}

	for _, msg := range params.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, openaisdk.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openaisdk.AssistantMessage(msg.Content))
		}
	}

	// Build request params.
	reqParams := openaisdk.ChatCompletionNewParams{
		Model:    openaisdk.ChatModel(model),
		Messages: messages,
	}

	if params.MaxTokens > 0 {
		reqParams.MaxCompletionTokens = openaisdk.Int(int64(params.MaxTokens))
	}

	if params.Temperature != nil {
		reqParams.Temperature = openaisdk.Float(*params.Temperature)
	}

	// Execute the API call.
	resp, err := p.client.Chat.Completions.New(ctx, reqParams)
	if err != nil {
		return providers.CallResult{}, classifyError(model, err)
	}

	// Map response to standardised CallResult.
	result := providers.CallResult{
		InputTokens:  int64(resp.Usage.PromptTokens),
		OutputTokens: int64(resp.Usage.CompletionTokens),
		Model:        resp.Model,
		RawResponse:  resp,
	}

	// OpenAI reports cached tokens in prompt_tokens_details.
	if resp.Usage.PromptTokensDetails.CachedTokens > 0 {
		result.CachedTokens = int64(resp.Usage.PromptTokensDetails.CachedTokens)
	}

	return result, nil
}

// classifyError wraps raw errors with typed Otellix error types.
func classifyError(model string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &providers.TimeoutError{Provider: "openai", Model: model, Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &providers.TimeoutError{Provider: "openai", Model: model, Err: err}
	}

	var httpErr interface{ StatusCode() int }
	if errors.As(err, &httpErr) {
		if httpErr.StatusCode() == http.StatusTooManyRequests {
			return &providers.RateLimitError{Provider: "openai", Model: model, Err: err}
		}
	}

	return &providers.ProviderError{
		Provider: "openai",
		Model:    model,
		Err:      fmt.Errorf("API call failed: %w", err),
	}
}

// Stream sends a chat completion request to the OpenAI API and streams the response back.
func (p *Provider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	model := params.Model
	if model == "" {
		model = "gpt-4o"
	}

	// Build messages.
	messages := make([]openaisdk.ChatCompletionMessageParamUnion, 0, len(params.Messages)+1)
	if params.SystemPrompt != "" {
		messages = append(messages, openaisdk.SystemMessage(params.SystemPrompt))
	}
	for _, msg := range params.Messages {
		switch msg.Role {
		case "user":
			messages = append(messages, openaisdk.UserMessage(msg.Content))
		case "assistant":
			messages = append(messages, openaisdk.AssistantMessage(msg.Content))
		}
	}

	// Build request params.
	reqParams := openaisdk.ChatCompletionNewParams{
		Model:    openaisdk.ChatModel(model),
		Messages: messages,
	}
	if params.MaxTokens > 0 {
		reqParams.MaxCompletionTokens = openaisdk.Int(int64(params.MaxTokens))
	}
	if params.Temperature != nil {
		reqParams.Temperature = openaisdk.Float(*params.Temperature)
	}

	// Execute the streaming API call.
	stream := p.client.Chat.Completions.NewStreaming(ctx, reqParams)

	return &openaiStream{
		stream: stream,
		model:  model,
	}, nil
}

type openaiStream struct {
	stream *ssestream.Stream[openaisdk.ChatCompletionChunk]
	model  string
}

func (s *openaiStream) Recv() (providers.StreamEvent, error) {
	if !s.stream.Next() {
		if err := s.stream.Err(); err != nil {
			return providers.StreamEvent{}, classifyError(s.model, err)
		}
		return providers.StreamEvent{}, fmt.Errorf("stream closed")
	}

	chunk := s.stream.Current()
	event := providers.StreamEvent{}

	if len(chunk.Choices) > 0 {
		event.Token = chunk.Choices[0].Delta.Content
	}

	// OpenAI usually sends usage in the last chunk.
	if chunk.Usage.PromptTokens > 0 {
		event.InputTokens = int64(chunk.Usage.PromptTokens)
		event.OutputTokens = int64(chunk.Usage.CompletionTokens)
	}

	return event, nil
}

func (s *openaiStream) Close() error {
	return s.stream.Close()
}
