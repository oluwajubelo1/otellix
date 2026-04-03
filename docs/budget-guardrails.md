# Budget Guardrails & Policies

Otellix provides a unique enforcement layer that goes beyond simple cost-tracking. It allows you to set hard or soft limits on LLM usage to prevent runaway costs per-user or per-project.

## Key Concepts

### 1. The Budget Config (`otellix.BudgetConfig`)
The central policy object. It defines how limits are calculated and what action to take when those limits are breached.

```go
config := &otellix.BudgetConfig{
    Store:                otellix.NewInMemoryBudgetStore(24 * time.Hour),
    PerUserDailyLimit:    1.00,  // $1.00 / day
    PerProjectDailyLimit: 25.00, // $25.00 / day
    FallbackAction:       otellix.FallbackBlock,
}
```

### 2. The Budget Store (`otellix.BudgetStore`)
Where usage metrics are persisted. By default, Otellix provides an `InMemoryBudgetStore` which is perfect for dev/test or single-instance applications. For high-availability production environments, you can implement a Redis or Database-backed store.

## Fallback Actions

| Action | Behaviour | Use Case |
| --- | --- | --- |
| `FallbackBlock` | Return `BudgetExceededError` immediately. LLM is never invoked. | Hard cost caps for untrusted users. |
| `FallbackNotify`| Allow the call but emit a `budget.warning` event on the trace. | Internal tools where you want to audit but not interrupt. |
| `FallbackDowngrade`| Automatically swap to a cheaper model (`Config.FallbackModel`). | Maintaining service availability while managing costs. |

## Implementation Patterns

### User-Level Guardrails
Otellix uses the `WithUserID` option to attribute costs. If a user hits their limit, Otellix ensures they cannot trigger further LLM spans.

```go
result, err := otellix.Trace(ctx, p, params,
    otellix.WithUserID("user_456"),
    otellix.WithBudgetConfig(myConfig),
)
```

### Real-Time Streaming Cutoff
When using `TraceStream`, Otellix tracks the cumulative cost of the tokens received. If the budget is hit **mid-stream**, Otellix sends an interrupt signal to the provider and returns a `BudgetExceededError` through the `Recv()` call.

## Dynamic Prompt Optimization
Using **Prompt Decorators**, you can modify the incoming LLM request based on the remaining budget.

```go
decorator := func(ctx context.Context, status otellix.BudgetStatus, params *providers.CallParams) {
    if status.Remaining < 0.05 {
        params.SystemPrompt += "\n[BUDGET WARNING: Please be highly concise.]"
        params.MaxTokens = 50 
    }
}

otellix.Trace(ctx, p, params,
    otellix.WithBudgetConfig(myConfig),
    otellix.WithPromptDecorator(decorator),
)
```

## Error Handling

When a budget is hit, Otellix returns a typed `BudgetExceededError`. You can use this to provide a custom UI message to the user.

```go
if errors.As(err, &budgetErr) {
    fmt.Printf("Limit reached. Spend: $%.2f, Limit: $%.2f, Resets at: %v",
        budgetErr.CurrentSpend, budgetErr.Limit, budgetErr.ResetAt)
}
```
