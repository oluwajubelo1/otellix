# HackerNews: Show HN Draft

**Title:** Show HN: Otellix – LLM observability SDK for Go with budget guardrails

---

**Post Content:**

Hi HN,

I’m Oluwasegun, a senior backend engineer based in Lagos, Nigeria. Over the last year, I’ve been integrating LLMs into education and civic tech platforms (Class54, BudgIT).

One of the biggest hurdles we faced wasn't just "which model to use," but **how to not go bankrupt.** In cost-constrained markets, a recursive prompt loop or a single high-volume user can wipe out our entire monthly API budget before we even notice.

I looked for Go-native observability tools that had cost control as a first-class feature, but most were either Python-first or focused on "vanity metrics."

So I built **Otellix**: https://github.com/oluwajubelo1/otellix

**What makes it different?**
- **Budget Guardrails**: This is the core. You can set daily/monthly USD limits per user or project feature. When a limit is hit, Otellix can automatically block the call, notify you via OTel events, or downgrade the request to a cheaper model (e.g., GPT-4o to GPT-4o-mini).
- **OpenTelemetry Native**: It’s 100% OTel compliant. It doesn't force a new dashboard on you; it just sends traces and metrics to your existing Prometheus/Grafana/Jaeger stack.
- **Go-First**: Built for Go 1.22+, focusing on native streaming, high concurrency, and minimal overhead.
- **Provider-Agnostic**: Supports OpenAI, Anthropic, Gemini, and Ollama (for local-first setups).

I built this primarily to solve my own "bill shock" problems in production, but I think the budget enforcement aspect is something every startup could use, regardless of their market.

It's MIT licensed. I’d love to hear your thoughts on the budget enforcement logic and what other "guardrails" would be useful in a production LLM stack.

Thanks!
