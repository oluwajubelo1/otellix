package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/anthropics/anthropic-sdk-go/option"
	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
	"github.com/oluwajubelo1/otellix/providers/gemini"
	"github.com/oluwajubelo1/otellix/providers/ollama"
	"github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
	var provider providers.Provider
	var model string
	var providerName string

	ctx := context.Background()

	// Detect which provider to use based on env vars
	if key := os.Getenv("ANTHROPIC_API_KEY"); key != "" {
		provider = anthropic.New(option.WithAPIKey(key))
		model = "claude-3-5-sonnet-20240620"
		providerName = "anthropic"
	} else if key := os.Getenv("OPENAI_API_KEY"); key != "" {
		provider = openai.New() // Reads from OPENAI_API_KEY by default
		model = "gpt-4o"
		providerName = "openai"
	} else if key := os.Getenv("GEMINI_API_KEY"); key != "" {
		var err error
		provider, err = gemini.New(ctx, key)
		if err != nil {
			fmt.Printf("Error initializing Gemini: %v\n", err)
			return
		}
		model = "gemini-1.5-flash"
		providerName = "gemini"
	} else if os.Getenv("USE_OLLAMA") == "true" {
		provider = ollama.New()
		model = "llama3.2"
		providerName = "ollama"
	} else {
		fmt.Println("Warning: No API keys found (ANTHROPIC_API_KEY, OPENAI_API_KEY, GEMINI_API_KEY).")
		fmt.Println("Using simulated real-time stream for demonstration purposes...")
		provider = &simulatedProvider{}
		model = "claude-3-5-sonnet-20240620"
		providerName = "anthropic"
	}

	// Setup stdout dev exporter to see OpenTelemetry spans at the end
	otellix.SetupDev()

	reader := bufio.NewReader(os.Stdin)
	fmt.Printf("\n=== Otellix Real-time Streaming [%s] ===\n", providerName)
	fmt.Println("Type a prompt and watch tokens stream live.")
	fmt.Print("\nPrompt > ")

	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// Setup a tiny budget to demonstrate active mid-stream cutoff
	budgetCfg := &otellix.BudgetConfig{
		Store:                otellix.NewInMemoryBudgetStore(24 * time.Hour),
		PerProjectDailyLimit: 0.001, // $0.001 limit
		FallbackAction:       otellix.FallbackBlock,
	}

	// Call TraceStream
	stream, err := otellix.TraceStream(
		ctx,
		provider,
		providers.CallParams{
			Model: model,
			Messages: []providers.Message{
				{Role: "user", Content: text},
			},
			MaxTokens: 500,
		},
		otellix.WithProjectID("streaming-demo"),
		otellix.WithBudgetConfig(budgetCfg),
	)

	if err != nil {
		fmt.Printf("\nFailed to start stream: %v\n", err)
		return
	}
	defer stream.Close()

	fmt.Println("\n--- Assistant Response ---")

	var tokensSoFar int64
	var costSoFar float64

	for {
		evt, err := stream.Recv()
		if err != nil {
			if strings.Contains(strings.ToLower(err.Error()), "budget") {
				fmt.Printf("\n\n[!] STREAM ABORTED: Budget Limit Reached! 🛑\n")
			} else if err.Error() == "EOF" || strings.Contains(err.Error(), "stream closed") {
				fmt.Println("\n\n[✓] Stream finished.")
			} else {
				fmt.Printf("\n[Error] %v\n", err)
			}
			break
		}

		fmt.Print(evt.Token)

		// Estimate tokens if provider doesn't report them per-chunk
		if evt.OutputTokens > 0 {
			tokensSoFar = evt.OutputTokens
		} else if evt.Token != "" {
			tokensSoFar++
		}

		// Calculate live cost periodically
		if tokensSoFar%10 == 0 {
			costSoFar = otellix.CalculateCost(providerName, model, providers.CallResult{
				OutputTokens: tokensSoFar,
			})
		}
	}

	fmt.Printf("\nFinal tokens: %d | Final estimated cost: $%.6f\n", tokensSoFar, costSoFar)
	fmt.Println("--- End Demo ---")
}

// --- Simulated Provider for Testing ---

type simulatedProvider struct{}

func (s *simulatedProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	return providers.CallResult{}, fmt.Errorf("simulated call not implemented")
}
func (s *simulatedProvider) Name() string { return "anthropic" }

type simulatedStream struct {
	tokens []string
	idx    int
}

func (s *simulatedStream) Recv() (providers.StreamEvent, error) {
	time.Sleep(30 * time.Millisecond)
	if s.idx >= len(s.tokens) {
		return providers.StreamEvent{}, fmt.Errorf("EOF")
	}
	tok := s.tokens[s.idx]
	s.idx++
	evt := providers.StreamEvent{Token: tok}
	if s.idx == len(s.tokens) {
		evt.OutputTokens = int64(len(s.tokens))
	} else if s.idx == 1 {
		evt.InputTokens = 10
	}
	return evt, nil
}
func (s *simulatedStream) Close() error { return nil }

func (s *simulatedProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	story := "As the codebase flickered onto his screen in the dead of the night, " +
		"a sudden realization struck the developer. The array indexes were completely misaligned! " +
		"He scrambled to fix the problem, typing fiercely. Every second cost him precious budget, " +
		"yet he couldn't stop. The LLM was helping, but it rambled on and on, explaining things " +
		"he didn't need to hear. Still, the streaming letters continued pouring in..."
	tokens := strings.Split(story, " ")
	for i := range tokens {
		tokens[i] += " "
	}
	return &simulatedStream{tokens: tokens}, nil
}
