package main

import (
	"context"
	"fmt"
	"log"
	"os"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/integrations/langchaingo"
	"github.com/tmc/langchaingo/llms"
	"github.com/tmc/langchaingo/llms/openai"
)

func main() {
	// 1. Initialise Otellix Dev mode to see traces in the console.
	shutdown := otellix.SetupDev()
	defer shutdown()

	apiKey := os.Getenv("OPENAI_API_KEY")
	if apiKey == "" {
		fmt.Println("⚠️ OPENAI_API_KEY not set. The GenerateContent call below will fail with an auth error.")
		fmt.Println("   Set OPENAI_API_KEY to run the real integration demo.")
	}

	// 2. Initialise the LangChainGo OpenAI LLM with the Otellix Callback Handler.
	// LangChainGo does not expose provider/model in its callbacks, so we
	// configure them explicitly to enable accurate cost tracking.
	otellixHandler := langchaingo.NewOtellixHandler(
		langchaingo.WithProvider("openai"),
		langchaingo.WithModel("gpt-4o"),
	)
	llm, err := openai.New(openai.WithCallback(otellixHandler))
	if err != nil {
		log.Fatalf("Failed to create OpenAI LLM: %v", err)
	}

	ctx := context.Background()

	fmt.Println("🚀 LangChainGo + Otellix Demo")
	fmt.Println("----------------------------")

	// 4. Run a simple generation with the handler attached.
	// user_id and project_id are automatically injected via the context
	// if we wrap it using otellix helpers.
	ctx = otellix.ContextWithUser(ctx, "user_langchain_fan")
	ctx = otellix.ContextWithProject(ctx, "proj_integration_test")

	fmt.Println("Sending request to OpenAI via LangChainGo...")

	// We pass our handler in the call options.
	resp, err := llm.GenerateContent(ctx, []llms.MessageContent{
		llms.TextParts(llms.ChatMessageTypeHuman, "Tell me a 5-word story about Go concurrency."),
	})

	if err != nil {
		log.Fatalf("LLM call failed: %v", err)
	}

	// 5. Success!
	fmt.Println("\nResponse:")
	for _, choice := range resp.Choices {
		fmt.Printf("> %s\n", choice.Content)
	}

	fmt.Println("\n✅ Trace captured at Otellix. Check console output above.")
}
