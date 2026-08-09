# Reddit Draft: r/golang

**Title Suggestions:**
- Show Reddit: Otellix – Production LLM observability for Go backends (OTel native)
- Tired of using Python tools for LLM monitoring? I built a Go-native OTel SDK
- Otellix: LLM Observability & Budget Guardrails for Go (built for cost-constrained markets)

---

**Post Content:**

Hi Gophers,

I’ve been integrating LLMs (OpenAI, Anthropic, Gemini) into several production Go services recently. One thing that consistently frustrated me was the lack of native observability tooling. Most of the popular frameworks (LangSmith, Helicone, etc.) are heavily focused on Python/Node or require weird proxy setups.

I wanted something that felt like it belonged in a Go codebase:
- **OpenTelemetry Native**: No custom dashboards required; just use your existing Prometheus/Grafana stack.
- **Fast & Concurrent**: Designed with Go 1.22+ paradigms (native streaming, context-aware).
- **Cost Enforcement**: Real-time USD budget tracking at the middleware layer to prevent "bill shock."

So I built **Otellix**.

### Core Philosophy
Otellix isn't just about showing you a graph. It's about bringing cost as a first-class concern to your application logic. If a user exceeds their daily LLM budget, Otellix can automatically:
1. Block the call.
2. Downgrade them to a cheaper model (e.g., GPT-4o -> GPT-4o-mini).
3. Notify your system via OTel events.

### Example: Gin Middleware Identity Attribution
No more manual context plumbing. Otellix extracts user/project IDs from headers and attaches them to your LLM spans.

```go
r := gin.Default()
r.Use(otellixgin.Middleware())

// Calls inside the handler automatically pick up the User/Project identity
result, err := otellix.Trace(ctx, p, params)
```

I originally built this as a Senior Backend Engineer managing LLM costs for platforms in Nigeria (Class54, BudgIT), where a runaway GPT-4 loop can wipe out a monthly budget in hours. 

It’s fully open-source (MIT), and I’m looking for feedback from the Go community on what features are missing for production deployments.

**GitHub**: [https://github.com/oluwajubelo1/otellix](https://github.com/oluwajubelo1/otellix)

I'll be around to answer any questions!
