---
title: "Productionizing LLMs in Go: Distributed Budgeting, LangChain, and ROI Tracking"
published: false
description: "How to take Otellix to production with distributed budget enforcement, automated identity discovery, and LangChainGo integration."
tags: "go, o11y, langchain, ai"
---

# From "Hello World" to Production: Stop the LLM Bill Shock for Good

A few weeks ago, we introduced **Otellix**—the Go-native observability SDK designed to help you stop bleeding money on LLMs. The response was incredible, but the feedback was clear: *"How do we scale this to production?"*

When you move from a single script to a distributed system with hundreds of users, cost management becomes a distributed systems problem. Today, I’m excited to share how we’ve leveled up Otellix to make it truly **production-ready**.

## 1. ⛓️ First-Class LangChainGo Support

Many of you are building complex AI agents using [LangChainGo](https://github.com/tmc/langchaingo). Tracking costs inside deeply nested chains and tool calls used to be a nightmare of manual context plumbing.

Not anymore. We’ve released the `OtellixHandler`, a drop-in callback for LangChainGo.

```go
import "github.com/oluwajubelo1/otellix/integrations/langchaingo"

// One line to hook it up
handler := langchaingo.NewOtellixHandler()
llm, _ := openai.New(openai.WithCallback(handler))

// Everything—from chains to tools—is now automatically traced and cost-tracked.
res, err := llm.GenerateContent(ctx, parts)
```

## 2. 🔌 Zero-Config Attribution (Middleware)

Manual attribution (passing `user_id` and `project_id` everywhere) is error-prone. In v0.2, we’ve added HTTP middleware for **Gin** and **Echo** that does the heavy lifting for you.

Simply add the middleware to your router, and Otellix will automatically "discover" the identity from JWTs and headers (like `X-Project-ID`) and carry it through the entire request lifecycle.

```go
import otellixmw "github.com/oluwajubelo1/otellix/middleware"

r := gin.Default()
r.Use(otellixmw.GinMiddleware()) // Automatically populates context with User/Project IDs
```

## 3. 🗄️ Distributed Budget Enforcement with Redis

In a distributed environment, an `InMemoryBudgetStore` isn't enough. You need a source of truth that spans your entire cluster. 

We’ve introduced the **Redis Budget Store**. Now, if `user_123` hits their $5.00 daily limit on Instance A, they are immediately blocked on Instance B. 

```go
store := redis.NewBudgetStore(redis.Config{
    Addr: "localhost:6379",
})

config := &otellix.BudgetConfig{
    Store: store,
    PerUserDailyLimit: 5.00,
}
```

## 4. 💎 Measuring Success: Prompt Caching ROI

Prompt caching (Anthropic & OpenAI) is the best way to save money, but how do you prove it's working?

Otellix now extracts granular caching metrics (`cache_read_tokens`, `cache_write_tokens`) and injects a new attribute into your OTel spans: `llm.savings_usd`. 

You can now build Grafana dashboards that show exactly how much your prompt engineering efforts are saving the company in real-time.

---

## What's Next?

We’re doubling down on **Local AI** support (enhanced Ollama metadata) and expanding to more web frameworks. 

If you're building LLM apps in Go, come say hi on [GitHub](https://github.com/oluwajubelo1/otellix)! We love stars, but we love PRs and feedback even more.

**Happy (and affordable) coding!** 🚀
