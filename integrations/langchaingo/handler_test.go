package langchaingo

import (
	"context"
	"fmt"
	"math"
	"testing"

	"github.com/oluwajubelo1/otellix"
	"github.com/tmc/langchaingo/llms"
	"go.opentelemetry.io/otel"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/trace"
)

func TestHandlerCreatesSpanAndRecordsUsage(t *testing.T) {
	exporter := &memoryExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx := otellix.ContextWithUser(context.Background(), "user_123")
	ctx = otellix.ContextWithProject(ctx, "proj_456")

	h := NewOtellixHandler(WithProvider("openai"), WithModel("gpt-4o"))
	h.HandleLLMGenerateContentStart(ctx, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "hi"),
	})
	h.HandleLLMGenerateContentEnd(ctx, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			Content: "hello",
			GenerationInfo: map[string]any{
				"PromptTokens":     10,
				"CompletionTokens": 5,
				"TotalTokens":      15,
			},
		}},
	})

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(exporter.spans))
	}
	sp := exporter.spans[0]
	if sp.Name() != "llm.call" {
		t.Errorf("expected span name llm.call, got %q", sp.Name())
	}
	attrs := sp.Attributes()
	got := map[string]string{}
	for _, kv := range attrs {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	expect := map[string]string{
		"llm.provider":           "openai",
		"llm.model":              "gpt-4o",
		"llm.input_tokens":       "10",
		"llm.output_tokens":      "5",
		"llm.user_id":            "user_123",
		"llm.project_id":         "proj_456",
		"llm.cache_read_tokens":  "0",
		"llm.cache_write_tokens": "0",
	}
	for k, want := range expect {
		if got[k] != want {
			t.Errorf("attribute %s = %q, want %q", k, got[k], want)
		}
	}
	// 10 input * $2.50/M + 5 output * $10/M = $0.000075
	if cost := float64FromEmit(got["llm.cost_usd"]); math.Abs(cost-0.000075) > 1e-9 {
		t.Errorf("llm.cost_usd = %v, want 0.000075", cost)
	}
}

func float64FromEmit(s string) float64 {
	var f float64
	_, _ = fmt.Sscanf(s, "%f", &f)
	return f
}

func TestHandlerAttachesToExistingSpan(t *testing.T) {
	exporter := &memoryExporter{}
	tp := sdktrace.NewTracerProvider(sdktrace.WithSyncer(exporter))
	otel.SetTracerProvider(tp)
	defer tp.Shutdown(context.Background())

	ctx, parent := otel.Tracer("test").Start(context.Background(), "parent")

	h := NewOtellixHandler(WithProvider("openai"), WithModel("gpt-4o"))
	h.HandleLLMGenerateContentEnd(ctx, &llms.ContentResponse{
		Choices: []*llms.ContentChoice{{
			GenerationInfo: map[string]any{"PromptTokens": 3, "CompletionTokens": 1},
		}},
	})
	parent.End()

	if len(exporter.spans) != 1 {
		t.Fatalf("expected 1 exported span, got %d", len(exporter.spans))
	}
	sp := exporter.spans[0]
	if sp.Name() != "parent" {
		t.Fatalf("expected handler to attach to existing span, got %q", sp.Name())
	}
	got := map[string]string{}
	for _, kv := range sp.Attributes() {
		got[string(kv.Key)] = kv.Value.Emit()
	}
	if got["llm.input_tokens"] != "3" || got["llm.output_tokens"] != "1" {
		t.Errorf("token attrs not recorded on existing span: %v", got)
	}
}

func TestProviderFromModel(t *testing.T) {
	cases := map[string]string{
		"gpt-4o":            "openai",
		"claude-3-5-sonnet": "anthropic",
		"gemini-1.5-pro":    "gemini",
		"llama-3.1-8b":      "ollama",
		"":                  "",
		"unknown-model":     "",
	}
	for model, want := range cases {
		if got := providerFromModel(model); got != want {
			t.Errorf("providerFromModel(%q) = %q, want %q", model, got, want)
		}
	}
}

type memoryExporter struct {
	spans []sdktrace.ReadOnlySpan
}

func (e *memoryExporter) ExportSpans(_ context.Context, spans []sdktrace.ReadOnlySpan) error {
	e.spans = append(e.spans, spans...)
	return nil
}

func (e *memoryExporter) Shutdown(context.Context) error { return nil }

var _ sdktrace.SpanExporter = (*memoryExporter)(nil)
var _ = trace.Span(nil)
