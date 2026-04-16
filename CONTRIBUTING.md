# Contributing to Otellix

We love evaluating pull requests! Whether you're fixing bugs, adding new LLM providers, or expanding budget guardrail strategies, your contributions shape the future of practical LLM observability. Otellix is engineered especially to assist developers facing scaling costs in budget-constrained ecosystems, so thoughtful optimisations are strictly welcomed.

## Development Setup

1. Check out the project:
   ```bash
   git clone https://github.com/oluwajubelo1/otellix.git
   cd otellix
   ```
2. Verify you're running Go 1.22+.

### Running robust tests

Every PR should ideally raise code coverage or maintain perfection across existing structures. To execute tests natively:

```bash
go test -v -race -cover ./...
```

If you are developing a new Provider wrapper (e.g. extending our Anthropic tracking API to another LLM environment), ensure you correctly satisfy the `providers.Provider` interface. Run the integration tests locally by supplying your personal API keys (if implementing actual network boundaries):

```bash
ANTHROPIC_API_KEY=your_key go run examples/realtime-stream/main.go
```

## Adding a new LLM provider

Implementing an untested LLM provider? Here is our design philosophy:
1. Wrap the provider using the `providers/provider.go` abstractions.
2. Build native mappings ensuring tokens (`InputTokens`, `OutputTokens`, `CacheReadTokens`, `CacheWriteTokens`) are explicitly collected securely inside your wrapper, alongside natively catching RateLimit and Timeout logic specifically.
3. Update the global pricing dictionary in `cost.go` dynamically with base metrics.
4. Open a Pull Request targeting `/feat/[featurename]`.

## Pull Requests
- We follow structured **Conventional Commits** (e.g. `feat: add mistral provider`, `fix: correctly account cache metrics`).
- Name your branches explicitly natively: `feat/[name]`, `bugfix/[desc]`, `chore/[name]`.
- All PRs immediately invoke the Github Actions `golangci-lint` workflow. Run `golangci-lint run ./...` manually before submitting to save pipeline time. 
