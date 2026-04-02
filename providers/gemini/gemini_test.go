package gemini_test

import (
	"context"
	"net/http"
	"testing"

	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/gemini"
	"github.com/oluwajubelo1/otellix/providers/providertest"
)

func TestGeminiCall(t *testing.T) {
	client := providertest.NewMockClient(providertest.NewJSONResponse(http.StatusOK, map[string]interface{}{
		"usageMetadata": map[string]int{"promptTokenCount": 10, "candidatesTokenCount": 20},
		"candidates":    []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]string{{"text": "Hello world"}}}}},
	}), nil)

	ctx := context.Background()
	p, err := gemini.New(ctx, "test-key", gemini.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	result, err := p.Call(ctx, providers.CallParams{
		Model:    "gemini-2.0-flash",
		Messages: []providers.Message{{Role: "user", Content: "Hi"}},
	})

	if err != nil {
		t.Fatalf("Call failed: %v", err)
	}

	if result.InputTokens != 10 || result.OutputTokens != 20 {
		t.Errorf("Unexpected tokens: %+v", result)
	}
}

/*
func TestGeminiStream(t *testing.T) {
	// The Google GenAI SDK (non-Vertex) REST streaming format expects
	// newline-delimited or whitespace-separated JSON objects.

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(http.StatusOK)

		// Object 1
		c1 := map[string]interface{}{
			"usageMetadata": map[string]int{"promptTokenCount": 10, "candidatesTokenCount": 0},
			"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]string{{"text": "Hello"}}}}},
		}
		json.NewEncoder(w).Encode(c1)

		// Object 2
		c2 := map[string]interface{}{
			"usageMetadata": map[string]int{"promptTokenCount": 10, "candidatesTokenCount": 20},
			"candidates": []map[string]interface{}{{"content": map[string]interface{}{"parts": []map[string]string{{"text": " world"}}}}},
		}
		json.NewEncoder(w).Encode(c2)
	}))
	defer server.Close()

	ctx := context.Background()
	client := &http.Client{
		Transport: &redirectTransport{BaseURL: server.URL},
	}

	p, err := gemini.New(ctx, "test-key", gemini.WithHTTPClient(client))
	if err != nil {
		t.Fatalf("New failed: %v", err)
	}

	stream, err := p.Stream(ctx, providers.CallParams{
		Model: "gemini-2.0-flash",
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
}
*/

type redirectTransport struct {
	BaseURL string
}

func (t *redirectTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	newReq := req.Clone(req.Context())
	newReq.URL.Scheme = "http"
	newReq.URL.Host = t.BaseURL[7:] // remove http://
	return http.DefaultTransport.RoundTrip(newReq)
}
