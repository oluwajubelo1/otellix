package openai_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/openai"
	"github.com/oluwajubelo1/otellix/providers/providertest"
	"github.com/openai/openai-go/option"
)

func TestOpenAICall(t *testing.T) {
	client := providertest.NewMockClient(providertest.NewJSONResponse(http.StatusOK, map[string]interface{}{
		"usage":   map[string]interface{}{"prompt_tokens": 10, "completion_tokens": 20, "prompt_tokens_details": map[string]int{"cached_tokens": 0}},
		"model":   "gpt-4o",
		"choices": []map[string]interface{}{{"message": map[string]string{"content": "Hello world"}}},
	}), nil)

	p := openai.New(option.WithHTTPClient(client), option.WithAPIKey("test-key"))

	result, err := p.Call(context.Background(), providers.CallParams{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result.InputTokens != 10 || result.OutputTokens != 20 {
		t.Errorf("Unexpected tokens: %+v", result)
	}
}

func TestOpenAIStream(t *testing.T) {
	// Simulate OpenAI SSE stream
	sseData := "data: {\"choices\": [{\"delta\": {\"content\": \"Hello\"}}]}\n\n" +
		"data: {\"choices\": [{\"delta\": {\"content\": \" world\"}}]}\n\n" +
		"data: {\"usage\": {\"prompt_tokens\": 10, \"completion_tokens\": 20}}\n\n" +
		"data: [DONE]\n\n"

	client := providertest.NewMockClient(providertest.NewStreamResponse(http.StatusOK, bytes.NewReader([]byte(sseData))), nil)

	p := openai.New(option.WithHTTPClient(client), option.WithAPIKey("test-key"))

	stream, err := p.Stream(context.Background(), providers.CallParams{
		Model:    "gpt-4o",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer stream.Close()

	var tokens []string
	var totalInput, totalOutput int64
	for {
		evt, err := stream.Recv()
		if err != nil {
			if err.Error() == "stream closed" || err == io.EOF {
				break
			}
			t.Fatalf("Recv failed: %v", err)
		}
		if evt.Token != "" {
			tokens = append(tokens, evt.Token)
		}
		if evt.InputTokens > 0 {
			totalInput = evt.InputTokens
		}
		if evt.OutputTokens > 0 {
			totalOutput = evt.OutputTokens
		}
	}

	if totalInput != 10 || totalOutput != 20 {
		t.Errorf("Unexpected final tokens: in=%d, out=%d", totalInput, totalOutput)
	}

	joined := ""
	for _, tok := range tokens {
		joined += tok
	}
	if joined != "Hello world" {
		t.Errorf("Unexpected response: %s", joined)
	}
}
