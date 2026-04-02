// Package ollama provides an Otellix provider wrapper for Ollama (local LLM inference).
package ollama

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"

	"github.com/oluwajubelo1/otellix/providers"
)

const defaultBaseURL = "http://localhost:11434"

// Provider wraps Ollama's HTTP API and implements providers.Provider.
// Ollama runs locally — all models are free ($0 cost).
type Provider struct {
	baseURL    string
	httpClient *http.Client
}

// Option is a functional option for the Ollama provider.
type Option func(*Provider)

// WithBaseURL sets the Ollama server address. Default: http://localhost:11434
func WithBaseURL(url string) Option {
	return func(p *Provider) { p.baseURL = url }
}

// WithHTTPClient sets a custom HTTP client.
func WithHTTPClient(client *http.Client) Option {
	return func(p *Provider) { p.httpClient = client }
}

// New creates a new Ollama provider.
func New(opts ...Option) *Provider {
	p := &Provider{
		baseURL:    defaultBaseURL,
		httpClient: http.DefaultClient,
	}
	for _, opt := range opts {
		opt(p)
	}
	return p
}

// Name returns the provider identifier.
func (p *Provider) Name() string { return "ollama" }

// ollamaRequest is the request body for Ollama's /api/chat endpoint.
type ollamaRequest struct {
	Model    string          `json:"model"`
	Messages []ollamaMessage `json:"messages"`
	Stream   bool            `json:"stream"`
	Options  *ollamaOptions  `json:"options,omitempty"`
}

type ollamaMessage struct {
	Role    string `json:"role"`
	Content string `json:"content"`
}

type ollamaOptions struct {
	Temperature *float64 `json:"temperature,omitempty"`
	NumPredict  int      `json:"num_predict,omitempty"`
}

// ollamaResponse is the response body from Ollama's /api/chat endpoint.
type ollamaResponse struct {
	Model   string        `json:"model"`
	Message ollamaMessage `json:"message"`

	PromptEvalCount int64 `json:"prompt_eval_count"`
	EvalCount       int64 `json:"eval_count"`
}

// Call sends a chat request to the local Ollama server.
func (p *Provider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	model := params.Model
	if model == "" {
		model = "llama3.2"
	}

	// Build messages.
	messages := make([]ollamaMessage, 0, len(params.Messages)+1)
	if params.SystemPrompt != "" {
		messages = append(messages, ollamaMessage{Role: "system", Content: params.SystemPrompt})
	}
	for _, msg := range params.Messages {
		messages = append(messages, ollamaMessage{Role: msg.Role, Content: msg.Content})
	}

	reqBody := ollamaRequest{
		Model:    model,
		Messages: messages,
		Stream:   false, // non-streaming for simplicity
	}

	if params.Temperature != nil || params.MaxTokens > 0 {
		reqBody.Options = &ollamaOptions{}
		if params.Temperature != nil {
			reqBody.Options.Temperature = params.Temperature
		}
		if params.MaxTokens > 0 {
			reqBody.Options.NumPredict = params.MaxTokens
		}
	}

	body, err := json.Marshal(reqBody)
	if err != nil {
		return providers.CallResult{}, fmt.Errorf("ollama: failed to marshal request: %w", err)
	}

	req, err := http.NewRequestWithContext(ctx, http.MethodPost, p.baseURL+"/api/chat", bytes.NewReader(body))
	if err != nil {
		return providers.CallResult{}, fmt.Errorf("ollama: failed to create request: %w", err)
	}
	req.Header.Set("Content-Type", "application/json")

	resp, err := p.httpClient.Do(req)
	if err != nil {
		return providers.CallResult{}, classifyError(model, err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		respBody, _ := io.ReadAll(resp.Body)
		return providers.CallResult{}, &providers.ProviderError{
			Provider: "ollama",
			Model:    model,
			Err:      fmt.Errorf("HTTP %d: %s", resp.StatusCode, string(respBody)),
		}
	}

	var ollamaResp ollamaResponse
	if err := json.NewDecoder(resp.Body).Decode(&ollamaResp); err != nil {
		return providers.CallResult{}, fmt.Errorf("ollama: failed to decode response: %w", err)
	}

	return providers.CallResult{
		InputTokens:  ollamaResp.PromptEvalCount,
		OutputTokens: ollamaResp.EvalCount,
		CachedTokens: 0,
		Model:        ollamaResp.Model,
		RawResponse:  ollamaResp,
	}, nil
}

// classifyError wraps raw errors with typed Otellix error types.
func classifyError(model string, err error) error {
	if err == nil {
		return nil
	}
	if errors.Is(err, context.DeadlineExceeded) {
		return &providers.TimeoutError{Provider: "ollama", Model: model, Err: err}
	}
	if errors.Is(err, context.Canceled) {
		return &providers.TimeoutError{Provider: "ollama", Model: model, Err: err}
	}
	return &providers.ProviderError{
		Provider: "ollama",
		Model:    model,
		Err:      fmt.Errorf("request failed: %w", err),
	}
}

// Stream sends a chat completion request to the Ollama API and streams the response back.
func (p *Provider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	return nil, errors.New("streaming not supported yet for ollama")
}
