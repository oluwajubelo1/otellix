package ollama_test

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/ollama"
)

func TestOllamaCall(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		resp := map[string]interface{}{
			"model":             "llama3",
			"message":           map[string]string{"role": "assistant", "content": "Hello"},
			"prompt_eval_count": 10,
			"eval_count":        20,
		}
		_ = json.NewEncoder(w).Encode(resp)
	}))
	defer server.Close()

	p := ollama.New(ollama.WithBaseURL(server.URL))

	result, err := p.Call(context.Background(), providers.CallParams{
		Model:    "llama3",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result.InputTokens != 10 || result.OutputTokens != 20 {
		t.Errorf("Unexpected tokens: %+v", result)
	}
}

func TestOllamaStream(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/x-ndjson")

		// Chunk 1
		_, _ = fmt.Fprintln(w, `{"message": {"content": "Hello"}, "done": false}`)
		// Chunk 2
		_, _ = fmt.Fprintln(w, `{"message": {"content": " world"}, "done": false}`)
		// Final Chunk
		_, _ = fmt.Fprintln(w, `{"message": {"content": ""}, "done": true, "prompt_eval_count": 10, "eval_count": 20}`)
	}))
	defer server.Close()

	p := ollama.New(ollama.WithBaseURL(server.URL))

	stream, err := p.Stream(context.Background(), providers.CallParams{
		Model:    "llama3",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})
	if err != nil {
		t.Fatalf("Stream failed: %v", err)
	}
	defer func() { _ = stream.Close() }()

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

func TestOllamaStreamHTTPError(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.WriteHeader(http.StatusNotFound)
		_, _ = fmt.Fprint(w, `{"error": "model 'missing' not found"}`)
	}))
	defer server.Close()

	p := ollama.New(ollama.WithBaseURL(server.URL))
	stream, err := p.Stream(context.Background(), providers.CallParams{Model: "missing"})
	if stream != nil {
		t.Error("expected no stream on an HTTP error")
	}

	var provErr *providers.ProviderError
	if !errors.As(err, &provErr) {
		t.Fatalf("expected *providers.ProviderError, got %T: %v", err, err)
	}
	if provErr.Provider != "ollama" || provErr.Model != "missing" {
		t.Errorf("unexpected provider error fields: %+v", provErr)
	}
	if !strings.Contains(err.Error(), "HTTP 404") || !strings.Contains(err.Error(), "not found") {
		t.Errorf("expected the status and response body in the error, got %q", err.Error())
	}
}
