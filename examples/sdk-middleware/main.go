// examples/sdk-middleware/main.go — Trace and budget an existing OpenAI SDK
// client by adding one middleware option. The calls themselves don't change.
//
// Run with:
//
//	OPENAI_API_KEY=xxx go run examples/sdk-middleware/main.go
//
// If OPENAI_API_KEY is not set, the client talks to a local fake API so the
// example runs anywhere without network access.
package main

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/http/httptest"
	"os"

	"github.com/openai/openai-go"
	"github.com/openai/openai-go/option"

	"github.com/oluwajubelo1/otellix"
	otellixopenai "github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
	shutdown := otellix.SetupDevConsole()
	defer shutdown()

	// $0.002 per user: enough for a call or two, then calls are blocked.
	budget := &otellix.BudgetConfig{
		PerUserLimit:   0.002,
		FallbackAction: otellix.FallbackBlock,
	}

	opts := []option.RequestOption{
		option.WithMiddleware(otellixopenai.Middleware(
			otellix.WithBudgetConfig(budget),
			otellix.WithFeatureID("sdk-middleware-demo"),
		)),
	}
	if os.Getenv("OPENAI_API_KEY") == "" {
		fmt.Println("OPENAI_API_KEY not set — using a local fake API")
		srv := fakeOpenAI()
		defer srv.Close()
		opts = append(opts, option.WithBaseURL(srv.URL), option.WithAPIKey("fake"))
	}
	client := openai.NewClient(opts...)

	// The user is read from the context, as web middleware would set it.
	ctx := otellix.ContextWithUser(context.Background(), "user_42")

	for i := 1; i <= 3; i++ {
		resp, err := client.Chat.Completions.New(ctx, openai.ChatCompletionNewParams{
			Model:     "gpt-4o",
			MaxTokens: openai.Int(100),
			Messages:  []openai.ChatCompletionMessageParamUnion{openai.UserMessage("What is OpenTelemetry in one sentence?")},
		})

		var budgetErr *otellix.BudgetExceededError
		if errors.As(err, &budgetErr) {
			fmt.Printf("call %d blocked: spent $%.4f of $%.4f\n", i, budgetErr.CurrentSpend, budgetErr.Limit)
			continue
		}
		if err != nil {
			log.Fatal(err)
		}
		fmt.Printf("call %d: %s\n", i, resp.Choices[0].Message.Content)
	}
}

// fakeOpenAI serves a fixed Chat Completions response.
func fakeOpenAI() *httptest.Server {
	return httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = fmt.Fprint(w, `{
			"id": "chatcmpl-1", "object": "chat.completion", "model": "gpt-4o",
			"choices": [{"index": 0, "finish_reason": "stop",
				"message": {"role": "assistant", "content": "OpenTelemetry is an open standard for collecting traces, metrics and logs."}}],
			"usage": {"prompt_tokens": 400, "completion_tokens": 30}
		}`)
	}))
}
