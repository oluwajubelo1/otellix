package anthropic_test

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"testing"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
	"github.com/oluwajubelo1/otellix/providers/providertest"
)

func TestAnthropicCall(t *testing.T) {
	client := providertest.NewMockClient(providertest.NewJSONResponse(http.StatusOK, map[string]interface{}{
		"usage":   map[string]int{"input_tokens": 10, "output_tokens": 20},
		"model":   "claude-3-5-sonnet",
		"content": []map[string]string{{"type": "text", "text": "Hello world"}},
	}), nil)

	p := anthropic.New(option.WithHTTPClient(client), option.WithAPIKey("test-key"))

	result, err := p.Call(context.Background(), providers.CallParams{
		Model:    "claude-3-5-sonnet",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result.InputTokens != 10 || result.OutputTokens != 20 {
		t.Errorf("Unexpected tokens: %+v", result)
	}
}

func TestAnthropicStream(t *testing.T) {
	// Simulate Anthropic SSE stream
	sseData := "event: message_start\ndata: {\"type\": \"message_start\", \"message\": {\"usage\": {\"input_tokens\": 10, \"output_tokens\": 0}}}\n\n" +
		"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"delta\": {\"type\": \"text_delta\", \"text\": \"Hello\"}}\n\n" +
		"event: content_block_delta\ndata: {\"type\": \"content_block_delta\", \"delta\": {\"type\": \"text_delta\", \"text\": \" world\"}}\n\n" +
		"event: message_delta\ndata: {\"type\": \"message_delta\", \"usage\": {\"output_tokens\": 20}}\n\n"

	client := providertest.NewMockClient(providertest.NewStreamResponse(http.StatusOK, bytes.NewReader([]byte(sseData))), nil)

	p := anthropic.New(option.WithHTTPClient(client), option.WithAPIKey("test-key"))

	stream, err := p.Stream(context.Background(), providers.CallParams{
		Model:    "claude-3-5-sonnet",
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
			if err == io.EOF {
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
