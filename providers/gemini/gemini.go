// Package gemini provides an Otellix provider wrapper for Google's Gemini API.
package gemini

import (
	"context"
	"errors"
	"fmt"

	"github.com/oluwajubelo1/otellix/providers"

	"google.golang.org/genai"
)

// Provider wraps the Google Generative AI SDK and implements providers.Provider.
type Provider struct {
	client *genai.Client
}

// New creates a new Gemini provider with the given API key.
func New(ctx context.Context, apiKey string) (*Provider, error) {
	client, err := genai.NewClient(ctx, &genai.ClientConfig{
		APIKey:  apiKey,
		Backend: genai.BackendGeminiAPI,
	})
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
	return nil, errors.New("streaming not supported yet for gemini")
}
