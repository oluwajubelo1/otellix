package main

import (
	"context"
	"fmt"
	"log"
	"time"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers/openai"
	otellixredis "github.com/oluwajubelo1/otellix/stores/redis"
	"github.com/redis/go-redis/v9"
)

func main() {
	// 1. Initialise the human-readable dev printer so we can see what's happening.
	shutdown := otellix.SetupDev()
	defer shutdown()

	// 2. Initialise Redis client.
	// Ensure you have Redis running: docker-compose -f docker-compose.dev.yml up -d
	rdb := redis.NewClient(&redis.Options{
		Addr: "localhost:6379",
	})
	ctx := context.Background()

	// Verify connection.
	if err := rdb.Ping(ctx).Err(); err != nil {
		log.Fatalf("Could not connect to Redis: %v. Please start Redis using the provided docker-compose file.", err)
	}

	fmt.Println("🚀 Otellix Redis Budget Demo")
	fmt.Println("---------------------------")

	// 3. Initialise RedisBudgetStore (24-hour rolling window).
	// This store can be shared across multiple application instances.
	store := otellixredis.NewRedisBudgetStore(rdb, "otellix:demo", 24*time.Hour)

	// 4. Configure Budget with a tiny limit to demonstrate enforcement.
	budgetCfg := &otellix.BudgetConfig{
		PerUserDailyLimit: 0.05, // $0.05 limit
		FallbackAction:    otellix.FallbackBlock,
		Store:             store,
	}

	// 5. Normal Tracer with OpenAI provider.
	provider := openai.New()

	for i := 1; i <= 10; i++ {
		fmt.Printf("\n--- Call #%d ---\n", i)

		_, err := otellix.Trace(ctx, provider, providers.CallParams{
			Model:    "gpt-4o",
			Messages: []providers.Message{{Role: "user", Content: "Tell me a short joke about Go."}},
		},
			otellix.WithUserID("user_redis_demo"),
			otellix.WithBudgetConfig(budgetCfg),
		)

		if err != nil {
			fmt.Printf("❌ Call Blocked: %v\n", err)

			// In a real app, you might want to see when the budget resets.
			if budgetErr, ok := err.(*otellix.BudgetExceededError); ok {
				fmt.Printf("⏳ Budget will reset at: %s\n", budgetErr.ResetAt.Format(time.Kitchen))
			}
			break
		}

		fmt.Println("✅ Call Successful (recorded in Redis)")
	}
}
