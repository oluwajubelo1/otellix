package tests

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/anthropics/anthropic-sdk-go"
	anthropicoption "github.com/anthropics/anthropic-sdk-go/option"
	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"
	"github.com/openai/openai-go/responses"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	otellixanthropic "github.com/oluwajubelo1/otellix/providers/anthropic"
	otellixopenai "github.com/oluwajubelo1/otellix/providers/openai"
)

// fakeAPI serves body for every request, with the given content type, and
// counts the requests.
func fakeAPI(t *testing.T, status int, contentType, body string) (*httptest.Server, *atomic.Int32) {
	t.Helper()
	var hits atomic.Int32
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		hits.Add(1)
		w.Header().Set("Content-Type", contentType)
		w.WriteHeader(status)
		_, _ = fmt.Fprint(w, body)
	}))
	t.Cleanup(srv.Close)
	return srv, &hits
}

// sse joins events into a server-sent event stream.
func sse(events ...string) string {
	return strings.Join(events, "\n\n") + "\n\n"
}

func newBudget() (*otellix.BudgetConfig, *otellix.InMemoryBudgetStore) {
	store := otellix.NewInMemoryBudgetStore(24 * time.Hour)
	return &otellix.BudgetConfig{PerUserLimit: 10, FallbackAction: otellix.FallbackBlock, Store: store}, store
}

func openAIClient(srv *httptest.Server, opts ...otellix.Option) openai.Client {
	return openai.NewClient(
		option.WithBaseURL(srv.URL),
		option.WithAPIKey("test-key"),
		option.WithMiddleware(otellixopenai.Middleware(opts...)),
	)
}

func anthropicClient(srv *httptest.Server, opts ...otellix.Option) anthropic.Client {
	return anthropic.NewClient(
		anthropicoption.WithBaseURL(srv.URL),
		anthropicoption.WithAPIKey("test-key"),
		anthropicoption.WithMiddleware(otellixanthropic.Middleware(opts...)),
	)
}

func assertSpend(t *testing.T, store *otellix.InMemoryBudgetStore, key string, want float64) {
	t.Helper()
	if spend := store.GetSpend(context.Background(), key); spend < want*0.999 || spend > want*1.001 {
		t.Errorf("expected spend $%.8f, got $%.8f", want, spend)
	}
}

var chatParams = openai.ChatCompletionNewParams{
	Model:    "gpt-4o",
	Messages: []openai.ChatCompletionMessageParamUnion{openai.UserMessage("hi")},
}

func TestOpenAIMiddlewareChatCompletion(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "application/json", `{
		"id": "c1", "object": "chat.completion", "model": "gpt-4o-2024-08-06",
		"choices": [{"index": 0, "message": {"role": "assistant", "content": "hello"}, "finish_reason": "stop"}],
		"usage": {"prompt_tokens": 1000, "completion_tokens": 50, "prompt_tokens_details": {"cached_tokens": 800}}
	}`)
	spans := recordSpans(t)
	budget, store := newBudget()
	client := openAIClient(srv, otellix.WithBudgetConfig(budget), otellix.WithFeatureID("chat"))

	ctx := otellix.ContextWithUser(context.Background(), "sdk-user")
	resp, err := client.Chat.Completions.New(ctx, chatParams)
	if err != nil {
		t.Fatal(err)
	}
	if resp.Choices[0].Message.Content != "hello" {
		t.Errorf("the SDK should still read the response, got %q", resp.Choices[0].Message.Content)
	}

	// 800 of the 1000 prompt tokens were cached, and are priced as such.
	assertSpend(t, store, "user:sdk-user", otellix.CalculateCost("openai", "gpt-4o",
		providers.CallResult{InputTokens: 200, OutputTokens: 50, CacheReadTokens: 800}))
	if got := spanAttr(t, spans, "llm.feature_id"); got != "chat" {
		t.Errorf("expected the span to carry the options, got feature_id %q", got)
	}
}

func TestOpenAIMiddlewareBudgetBlock(t *testing.T) {
	srv, hits := fakeAPI(t, http.StatusOK, "application/json", `{}`)
	budget, _ := newBudget()
	budget.PerUserLimit = 0.000001
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	start := time.Now()
	_, err := client.Chat.Completions.New(otellix.ContextWithUser(context.Background(), "broke"), chatParams)

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(err, &budgetErr) {
		t.Fatalf("expected a BudgetExceededError from the SDK, got %v", err)
	}
	if hits.Load() != 0 {
		t.Errorf("a blocked call should not be sent, got %d requests", hits.Load())
	}
	if elapsed := time.Since(start); elapsed > 300*time.Millisecond {
		t.Errorf("a blocked call should not be retried, took %v", elapsed)
	}
}

func TestOpenAIMiddlewareHTTPErrorPassesThrough(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusBadRequest, "application/json",
		`{"error": {"message": "bad model", "type": "invalid_request_error"}}`)
	budget, store := newBudget()
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	_, err := client.Chat.Completions.New(otellix.ContextWithUser(context.Background(), "u"), chatParams, option.WithMaxRetries(0))

	var apiErr *openai.Error
	if !errors.As(err, &apiErr) || apiErr.StatusCode != http.StatusBadRequest {
		t.Fatalf("expected the SDK's own 400 error, got %v", err)
	}
	assertSpend(t, store, "user:u", 0)
}

func TestOpenAIMiddlewareStreamUsage(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "text/event-stream", sse(
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"Hel"}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"lo"}}]}`,
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[],"usage":{"prompt_tokens":30,"completion_tokens":2}}`,
		`data: [DONE]`,
	))
	budget, store := newBudget()
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	stream := client.Chat.Completions.NewStreaming(otellix.ContextWithUser(context.Background(), "streamer"), chatParams)
	var text strings.Builder
	for stream.Next() {
		if c := stream.Current(); len(c.Choices) > 0 {
			text.WriteString(c.Choices[0].Delta.Content)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	if text.String() != "Hello" {
		t.Errorf("expected the SDK to read %q, got %q", "Hello", text.String())
	}
	assertSpend(t, store, "user:streamer", otellix.CalculateCost("openai", "gpt-4o",
		providers.CallResult{InputTokens: 30, OutputTokens: 2}))
}

// TestOpenAIMiddlewareStreamWithoutUsage checks that a stream with no usage
// chunk, the default for Chat Completions, is still charged an estimate.
func TestOpenAIMiddlewareStreamWithoutUsage(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "text/event-stream", sse(
		`data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"`+strings.Repeat("a", 400)+`"}}]}`,
		`data: [DONE]`,
	))
	budget, store := newBudget()
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	stream := client.Chat.Completions.NewStreaming(otellix.ContextWithUser(context.Background(), "estimated"), chatParams)
	for stream.Next() {
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	// 400 characters is about 100 output tokens; "hi" is about 5 input tokens.
	assertSpend(t, store, "user:estimated", otellix.CalculateCost("openai", "gpt-4o",
		providers.CallResult{InputTokens: 5, OutputTokens: 100}))
}

func TestOpenAIMiddlewareStreamCutoff(t *testing.T) {
	chunk := `data: {"id":"c1","object":"chat.completion.chunk","model":"gpt-4o","choices":[{"index":0,"delta":{"content":"` + strings.Repeat("a", 40) + `"}}]}`
	events := make([]string, 0, 21)
	for range 20 {
		events = append(events, chunk)
	}
	srv, _ := fakeAPI(t, http.StatusOK, "text/event-stream", sse(append(events, `data: [DONE]`)...))

	// Each chunk is about 10 output tokens, $0.0001 on gpt-4o. The limit
	// has room for about 3.
	budget, _ := newBudget()
	budget.PerUserLimit = 0.00035
	budget.EstimatedOutputTokens = 1
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	stream := client.Chat.Completions.NewStreaming(otellix.ContextWithUser(context.Background(), "cutoff"), chatParams)
	received := 0
	for stream.Next() {
		received++
	}
	_ = stream.Close()

	var budgetErr *otellix.BudgetExceededError
	if !errors.As(stream.Err(), &budgetErr) {
		t.Fatalf("expected the stream to end with a BudgetExceededError, got %v", stream.Err())
	}
	if received == 0 || received >= 20 {
		t.Errorf("expected the stream to be cut off part way, got %d of 20 chunks", received)
	}
}

func TestOpenAIMiddlewareResponsesStream(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "text/event-stream", sse(
		"event: response.created\ndata: {\"type\":\"response.created\",\"response\":{\"id\":\"r1\"}}",
		"event: response.output_text.delta\ndata: {\"type\":\"response.output_text.delta\",\"delta\":\"Hi there\"}",
		"event: response.completed\ndata: {\"type\":\"response.completed\",\"response\":{\"id\":\"r1\",\"usage\":{\"input_tokens\":120,\"output_tokens\":3,\"input_tokens_details\":{\"cached_tokens\":100}}}}",
	))
	budget, store := newBudget()
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	stream := client.Responses.NewStreaming(otellix.ContextWithUser(context.Background(), "responder"), responses.ResponseNewParams{
		Model: "gpt-4o",
		Input: responses.ResponseNewParamsInputUnion{OfString: openai.String("hi")},
	})
	for stream.Next() {
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	assertSpend(t, store, "user:responder", otellix.CalculateCost("openai", "gpt-4o",
		providers.CallResult{InputTokens: 20, OutputTokens: 3, CacheReadTokens: 100}))
}

func TestOpenAIMiddlewareIgnoresOtherEndpoints(t *testing.T) {
	srv, hits := fakeAPI(t, http.StatusOK, "application/json",
		`{"object":"list","data":[{"object":"embedding","index":0,"embedding":[0.1]}],"model":"text-embedding-3-small","usage":{"prompt_tokens":1,"total_tokens":1}}`)
	spans := recordSpans(t)
	client := openAIClient(srv)

	_, err := client.Embeddings.New(context.Background(), openai.EmbeddingNewParams{
		Model: "text-embedding-3-small",
		Input: openai.EmbeddingNewParamsInputUnion{OfString: openai.String("hi")},
	})
	if err != nil {
		t.Fatal(err)
	}
	if hits.Load() != 1 || len(spans.Ended()) != 0 {
		t.Errorf("expected the request to pass through untraced, got %d requests and %d spans", hits.Load(), len(spans.Ended()))
	}
}

var messageParams = anthropic.MessageNewParams{
	Model:     "claude-sonnet-4-6",
	MaxTokens: 1024,
	Messages:  []anthropic.MessageParam{anthropic.NewUserMessage(anthropic.NewTextBlock("hi"))},
}

func TestAnthropicMiddlewareMessage(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "application/json", `{
		"id": "m1", "type": "message", "role": "assistant", "model": "claude-sonnet-4-6",
		"content": [{"type": "text", "text": "hello"}], "stop_reason": "end_turn",
		"usage": {"input_tokens": 100, "output_tokens": 40, "cache_read_input_tokens": 2000, "cache_creation_input_tokens": 500}
	}`)
	budget, store := newBudget()
	client := anthropicClient(srv, otellix.WithBudgetConfig(budget))

	msg, err := client.Messages.New(otellix.ContextWithUser(context.Background(), "claude-user"), messageParams)
	if err != nil {
		t.Fatal(err)
	}
	if msg.Content[0].Text != "hello" {
		t.Errorf("the SDK should still read the response, got %q", msg.Content[0].Text)
	}
	assertSpend(t, store, "user:claude-user", otellix.CalculateCost("anthropic", "claude-sonnet-4-6",
		providers.CallResult{InputTokens: 100, OutputTokens: 40, CacheReadTokens: 2000, CacheWriteTokens: 500}))
}

func TestAnthropicMiddlewareStream(t *testing.T) {
	srv, _ := fakeAPI(t, http.StatusOK, "text/event-stream", sse(
		"event: message_start\ndata: {\"type\":\"message_start\",\"message\":{\"id\":\"m1\",\"type\":\"message\",\"role\":\"assistant\",\"model\":\"claude-sonnet-4-6\",\"content\":[],\"usage\":{\"input_tokens\":100,\"output_tokens\":1,\"cache_read_input_tokens\":2000}}}",
		"event: content_block_start\ndata: {\"type\":\"content_block_start\",\"index\":0,\"content_block\":{\"type\":\"text\",\"text\":\"\"}}",
		"event: content_block_delta\ndata: {\"type\":\"content_block_delta\",\"index\":0,\"delta\":{\"type\":\"text_delta\",\"text\":\"Hello\"}}",
		"event: content_block_stop\ndata: {\"type\":\"content_block_stop\",\"index\":0}",
		"event: message_delta\ndata: {\"type\":\"message_delta\",\"delta\":{\"stop_reason\":\"end_turn\"},\"usage\":{\"output_tokens\":12}}",
		"event: message_stop\ndata: {\"type\":\"message_stop\"}",
	))
	budget, store := newBudget()
	client := anthropicClient(srv, otellix.WithBudgetConfig(budget))

	stream := client.Messages.NewStreaming(otellix.ContextWithUser(context.Background(), "claude-streamer"), messageParams)
	var text strings.Builder
	for stream.Next() {
		if evt := stream.Current(); evt.Type == "content_block_delta" {
			text.WriteString(evt.Delta.Text)
		}
	}
	if err := stream.Err(); err != nil {
		t.Fatal(err)
	}
	_ = stream.Close()

	if text.String() != "Hello" {
		t.Errorf("expected the SDK to read %q, got %q", "Hello", text.String())
	}
	assertSpend(t, store, "user:claude-streamer", otellix.CalculateCost("anthropic", "claude-sonnet-4-6",
		providers.CallResult{InputTokens: 100, OutputTokens: 12, CacheReadTokens: 2000}))
}

// TestOpenAIMiddlewareDowngrade checks that FallbackDowngrade changes the
// model in the request the SDK sends, not only in Otellix's records.
func TestOpenAIMiddlewareDowngrade(t *testing.T) {
	var sentModel atomic.Value
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		var req struct {
			Model string `json:"model"`
		}
		_ = json.NewDecoder(r.Body).Decode(&req)
		sentModel.Store(req.Model)
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprintf(w, `{"id":"c1","object":"chat.completion","model":%q,
			"choices":[{"index":0,"message":{"role":"assistant","content":"hi"},"finish_reason":"stop"}],
			"usage":{"prompt_tokens":10,"completion_tokens":5}}`, req.Model)
	}))
	t.Cleanup(srv.Close)

	budget, store := newBudget()
	budget.PerUserLimit = 0.000001
	budget.FallbackAction = otellix.FallbackDowngrade
	budget.FallbackModel = "gpt-4o-mini"
	client := openAIClient(srv, otellix.WithBudgetConfig(budget))

	if _, err := client.Chat.Completions.New(otellix.ContextWithUser(context.Background(), "downgraded"), chatParams); err != nil {
		t.Fatal(err)
	}
	if got := sentModel.Load(); got != "gpt-4o-mini" {
		t.Errorf("expected the request to be sent with gpt-4o-mini, got %v", got)
	}
	assertSpend(t, store, "user:downgraded", otellix.CalculateCost("openai", "gpt-4o-mini",
		providers.CallResult{InputTokens: 10, OutputTokens: 5}))
}
