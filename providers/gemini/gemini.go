// Package gemini provides an Otellix provider wrapper for Google's Gemini API.
package gemini

import (
	"context"
	"errors"
	"fmt"
	"iter"
	"net/http"

	"github.com/oluwajubelo1/otellix/providers"

	"google.golang.org/genai"
)

// Provider wraps the Google Generative AI SDK and implements providers.Provider.
type Provider struct {
	client *genai.Client
}

// Option is a functional option for the Gemini provider.
type Option func(*genai.ClientConfig)

// WithHTTPClient sets a custom HTTP client for the Gemini provider.
func WithHTTPClient(client *http.Client) Option {
	return func(c *genai.ClientConfig) { c.HTTPClient = client }
}

// New creates a new Gemini provider with the given API key and options.
func New(ctx context.Context, apiKey string, opts ...Option) (*Provider, error) {
	config := &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	}

	for _, opt := range opts {
		opt(config)
	}

	client, err := genai.NewClient(ctx, config)
	if err != nil {
		return nil, fmt.Errorf("gemini: failed to create client: %w", err)
	}
	return &Provider{client: client}, nil
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "gemini" }

// Call sends a request to the Gemini API and returns a standardised CallResult.
func (p *Provider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	model := params.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	// Build the content parts.
	var parts []*genai.Part
	for _, msg := range params.Messages {
		p := genai.NewPartFromText(msg.Content)
		parts = append(parts, p)
	}

	// Build contents as []*Content.
	contents := []*genai.Content{
		genai.NewContentFromParts(parts, genai.RoleUser),
	}

	// Build config.
	config := &genai.GenerateContentConfig{}
	if params.MaxTokens > 0 {
		maxTokens := int32(params.MaxTokens)
		config.MaxOutputTokens = maxTokens
	}
	if params.Temperature != nil {
		temp := float32(*params.Temperature)
		config.Temperature = &temp
	}
	if params.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(params.SystemPrompt)},
		}
	}

	// Execute the API call.
	resp, err := p.client.Models.GenerateContent(ctx, model, contents, config)
	if err != nil {
		return providers.CallResult{}, classifyError(model, err)
	}

	// Map response to standardised CallResult.
	result := providers.CallResult{
		Model:       model,
		RawResponse: resp,
	}

	if resp.UsageMetadata != nil {
		result.InputTokens = int64(resp.UsageMetadata.PromptTokenCount)
		result.OutputTokens = int64(resp.UsageMetadata.CandidatesTokenCount)
		result.CachedTokens = int64(resp.UsageMetadata.CachedContentTokenCount)
	}

	return result, nil
}

// classifyError wraps raw errors with typed Otellix error types.
func classifyError(model string, err error) error {
	if err == nil {
		return nil
	}

	if errors.Is(err, context.DeadlineExceeded) {
		return &providers.TimeoutError{Provider: "gemini", Model: model, Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &providers.TimeoutError{Provider: "gemini", Model: model, Err: err}
	}

	return &providers.ProviderError{
		Provider: "gemini",
		Model:    model,
		Err:      fmt.Errorf("API call failed: %w", err),
	}
}

// Stream sends a chat completion request to the Gemini API and streams the response back.
func (p *Provider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	model := params.Model
	if model == "" {
		model = "gemini-2.5-flash"
	}

	// Build the content parts.
	var parts []*genai.Part
	for _, msg := range params.Messages {
		p := genai.NewPartFromText(msg.Content)
		parts = append(parts, p)
	}

	contents := []*genai.Content{
		genai.NewContentFromParts(parts, genai.RoleUser),
	}

	config := &genai.GenerateContentConfig{}
	if params.MaxTokens > 0 {
		config.MaxOutputTokens = int32(params.MaxTokens)
	}
	if params.Temperature != nil {
		temp := float32(*params.Temperature)
		config.Temperature = &temp
	}
	if params.SystemPrompt != "" {
		config.SystemInstruction = &genai.Content{
			Parts: []*genai.Part{genai.NewPartFromText(params.SystemPrompt)},
		}
	}

	// Execute streaming.
	seq := p.client.Models.GenerateContentStream(ctx, model, contents, config)
	next, stop := iter.Pull2(seq)

	return &geminiStream{
		next:  next,
		stop:  stop,
		model: model,
	}, nil
}

type geminiStream struct {
	next  func() (*genai.GenerateContentResponse, error, bool)
	stop  func()
	model string
}

func (s *geminiStream) Recv() (providers.StreamEvent, error) {
	resp, err, ok := s.next()
	if !ok {
		return providers.StreamEvent{}, fmt.Errorf("stream closed")
	}
	if err != nil {
		return providers.StreamEvent{}, classifyError(s.model, err)
	}

	event := providers.StreamEvent{}
	if len(resp.Candidates) > 0 && len(resp.Candidates[0].Content.Parts) > 0 {
		event.Token = resp.Candidates[0].Content.Parts[0].Text
	}

	if resp.UsageMetadata != nil {
		event.InputTokens = int64(resp.UsageMetadata.PromptTokenCount)
		event.OutputTokens = int64(resp.UsageMetadata.CandidatesTokenCount)
	}

	return event, nil
}

func (s *geminiStream) Close() error {
	s.stop()
	return nil
}
