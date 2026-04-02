package main

import (
	"bufio"
	"context"
	"fmt"
	"os"
	"strings"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
	"github.com/anthropics/anthropic-sdk-go/option"
)

func main() {
	var provider providers.Provider
	apiKey := os.Getenv("ANTHROPIC_API_KEY")
	if apiKey == "" {
		fmt.Println("Warning: ANTHROPIC_API_KEY not found. Using simulated real-time stream...")
		provider = &simulatedProvider{}
	} else {
		provider = anthropic.New(option.WithAPIKey(apiKey))
	}

	// Setup stdout dev exporter so we see everything at the end
	otellix.SetupDev()

	reader := bufio.NewReader(os.Stdin)
	fmt.Println("=== Otellix Real-time Streaming ===")
	fmt.Println("Type a prompt and press Enter. The cost and tokens will update live.")
	fmt.Println("Try asking for a very long response to watch the budget increase!")
	fmt.Print("\nPrompt > ")

	text, _ := reader.ReadString('\n')
	text = strings.TrimSpace(text)
	if text == "" {
		return
	}

	// For demonstration, let's set a tiny budget to see it actively cut off mid-stream
	// if the model talks too much!
	budgetCfg := &otellix.BudgetConfig{
		Store:                otellix.NewInMemoryBudgetStore(24 * time.Hour),
		PerProjectDailyLimit: 0.0005, // super tiny limit so it cuts off our 60-word story
		FallbackAction:       otellix.FallbackBlock,
	}

	ctx := context.Background()

	// Call TraceStream
	stream, err := otellix.TraceStream(
		ctx,
		provider,
		providers.CallParams{
			Model: "claude-sonnet-4-6",
			Messages: []providers.Message{
				{Role: "user", Content: text},
			},
			MaxTokens: 2000,
		},
		otellix.WithProjectID("demo-proj"),
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
	
	// We'll use a simple ANSI escape trick to print the token at the top 
	// and keep a progress line at the very bottom.
	// But to keep it robust across terminals, let's just print the token normally,
	// and we won't print the cost every single token, instead we print it if it changes.
	
	for {
		evt, err := stream.Recv()
		if err != nil {
			if strings.Contains(err.Error(), "budget") || strings.Contains(err.Error(), "Budget") {
				fmt.Printf("\n\n[!] STREAM ABORTED MID-GENERATION: %v\n", err)
			} else if err.Error() == "EOF" {
				fmt.Println("\n\n[✓] Stream finished.")
			} else {
				fmt.Printf("\n[Error] %v\n", err)
			}
			break
		}
		
		fmt.Print(evt.Token)
		
		tokensSoFar += evt.OutputTokens
		if evt.Token != "" && evt.OutputTokens == 0 {
			tokensSoFar++
		}
		
		if tokensSoFar%25 == 0 {
			costSoFar = otellix.CalculateCost("anthropic", "claude-sonnet-4-6", providers.CallResult{OutputTokens: tokensSoFar})
			// Print a quick ephemeral status using carriage return (only works if we're on a separate line)
			// Actually let's just emit an invisible ANSI sequence to set the title of the terminal
			// fmt.Printf("\033]0;Otellix Live: %d tokens | $%.4f\007", tokensSoFar, costSoFar)
		}
	}
	
	// Print final cost manually since stream closed
	fmt.Printf("\nFinal estimated cost inside loop: $%.6f\n", costSoFar)
	fmt.Println("--- End ---")
}

// --- Simulated Provider for Testing ---

type simulatedProvider struct{}

func (s *simulatedProvider) Call(ctx context.Context, params providers.CallParams) (providers.CallResult, error) {
	return providers.CallResult{}, fmt.Errorf("not implemented")
}
func (s *simulatedProvider) Name() string { return "anthropic" }

type simulatedStream struct {
	tokens []string
	idx    int
}

func (s *simulatedStream) Recv() (providers.StreamEvent, error) {
	time.Sleep(50 * time.Millisecond) // Simulate network delay
	if s.idx >= len(s.tokens) {
		importIOError := struct { // Need io.EOF without adding extra imports awkwardly, but wait we didn't import io.
			// Let's just return a generic error called EOF
		}{}
		_ = importIOError
		return providers.StreamEvent{}, fmt.Errorf("EOF")
	}
	tok := s.tokens[s.idx]
	s.idx++
	evt := providers.StreamEvent{Token: tok}
	if s.idx == len(s.tokens) {
		// at end, return full output token usage
		evt.OutputTokens = int64(len(s.tokens))
	} else if s.idx == 1 {
		evt.InputTokens = 15 // Mock input
	}
	return evt, nil
}
func (s *simulatedStream) Close() error { return nil }

func (s *simulatedProvider) Stream(ctx context.Context, params providers.CallParams) (providers.Stream, error) {
	// A long mock response simulating an LLM generating lots of text slowly
	story := "As the codebase flickered onto his screen in the dead of the night, " +
		"a sudden realization struck the developer. The array indexes were completely misaligned! " +
		"He scrambled to fix the problem, typing fiercely. Every second cost him precious budget, " +
		"yet he couldn't stop. The LLM was helping, but it rambled on and on, explaining things " +
		"he didn't need to hear. Still, the streaming letters continued pouring in..."
	words := strings.Split(story, " ")
	tokens := make([]string, 0, len(words))
	for i, w := range words {
		if i > 0 {
			tokens = append(tokens, " ")
		}
		tokens = append(tokens, w)
	}

	return &simulatedStream{tokens: tokens}, nil
}

