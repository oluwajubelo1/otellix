# Verified Identity & Budgets

A per-user or per-project budget is only as strong as the ID it's attached to. If a caller can choose their own user ID, they can send a new one with every request and get a fresh budget each time. If they can choose the project ID, they can spend someone else's budget.

From v1.8.0, **budgets only apply limits to verified IDs**. This guide explains what counts as verified, how to set it up, and how to upgrade.

Related guides:

- [Middleware & Identity](middleware.md): setting up the Gin, Echo and Fiber middleware.
- [Cost Attribution](attribution.md): how calls get a user and project.
- [Budget Guardrails](budget-guardrails.md): limits and fallback actions.

---

## The rule

> A budget never applies a limit to an ID a caller could have chosen. If a limit would depend on an unverified ID, the call is refused with `*otellix.UnverifiedIdentityError`, and the provider is never called.

Unverified IDs still **attribute** calls. They're recorded on spans and in metrics, so you can see who is spending without verifying anything. Verification only matters once you enforce budgets.

---

## What counts as verified

| Where the ID comes from | Verified? |
| --- | --- |
| `otellix.WithUserID(...)` / `otellix.WithProjectID(...)` in your code | Yes |
| `otellix.ContextWithUser(...)` / `otellix.ContextWithProject(...)` in your code | Yes |
| Middleware with `WithUserFromContext(key)` / `WithProjectFromContext(key)`: read from your auth middleware | Yes |
| Middleware with `WithVerifiedJWT(...)`: a JWT whose signature and expiry Otellix checked | Yes |
| Middleware default: the JWT's `sub` claim, read **without** checking the signature | **No** |
| Middleware with `WithProjectHeader(name)`: a request header | **No** |
| `otellix.ContextWithUnverifiedUser(...)` / `ContextWithUnverifiedProject(...)` | **No** |

IDs set by your code count as verified because your code is vouching for them. Only set them from something you've checked, such as the signed-in user's session.

When both a verified and an unverified ID are present, the verified one wins: an option beats the context, and `ContextWithUser` beats `ContextWithUnverifiedUser`.

---

## When a call is refused

A call is refused only when a limit would actually depend on the unverified ID:

| The call has | The budget sets | Result |
| --- | --- | --- |
| An unverified user | `PerUserLimit` | Refused: a new user ID would get a fresh limit |
| An unverified user | `UnattributedLimit` | Refused: any user ID would escape the shared limit for calls with no user |
| An unverified user | Only `PerProjectLimit` | Allowed: the user ID doesn't affect any limit |
| An unverified project | `PerProjectLimit` | Refused: the caller could pick another project's budget |
| An unverified project | Only user limits | Allowed |
| Any ID | No `BudgetConfig` | Allowed: nothing to enforce |

The error says which ID it was:

```go
var unverified *otellix.UnverifiedIdentityError
if errors.As(err, &unverified) {
    log.Printf("budget refused unverified %s ID %q", unverified.Kind, unverified.ID) // Kind: "user" or "project"
}
```

This is a setup problem rather than something to show your users. In development, you'll see it on the first budgeted call, which is the point.

---

## Setting up verified users

Pick one of these.

### Option A: read the user from your auth middleware (recommended)

If your app already authenticates requests, reuse that. Have your auth middleware store the user ID, then point Otellix at it:

```go
r := gin.Default()

r.Use(func(c *gin.Context) {
    claims, err := verifyToken(c.GetHeader("Authorization")) // your real verification
    if err != nil {
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }
    c.Set("user_id", claims.Subject)
    c.Next()
})

r.Use(otellixmw.GinMiddleware(otellixmw.WithUserFromContext("user_id")))
```

Register the Otellix middleware **after** your auth middleware, so the ID is there when it looks. Echo uses `c.Set` the same way, and Fiber uses `c.Locals`. See [Middleware & Identity](middleware.md).

### Option B: have Otellix verify the JWT

If you don't have auth middleware that exposes the user, Otellix can verify the token itself. Give it the key to check signatures with:

```go
import jwt "github.com/golang-jwt/jwt/v5"

secret := []byte(os.Getenv("JWT_SECRET"))

r.Use(otellixmw.GinMiddleware(otellixmw.WithVerifiedJWT(
    func(*jwt.Token) (any, error) { return secret, nil },
    jwt.WithValidMethods([]string{"HS256"}), // only accept the algorithm you sign with
)))
```

`WithVerifiedJWT` takes a [`jwt.Keyfunc`](https://pkg.go.dev/github.com/golang-jwt/jwt/v5#Keyfunc) and any [`jwt.ParserOption`](https://pkg.go.dev/github.com/golang-jwt/jwt/v5#ParserOption), such as `jwt.WithIssuer`, `jwt.WithAudience` or `jwt.WithLeeway`. Expired tokens are rejected.

For tokens from an identity provider such as Auth0, Okta, Clerk or Cognito, which publish their keys at a JWKS URL, use a JWKS library that gives you a `jwt.Keyfunc`, for example [`github.com/MicahParks/keyfunc/v3`](https://github.com/MicahParks/keyfunc):

```go
jwks, err := keyfunc.NewDefault([]string{"https://YOUR_DOMAIN/.well-known/jwks.json"})
if err != nil {
    log.Fatal(err)
}

r.Use(otellixmw.GinMiddleware(otellixmw.WithVerifiedJWT(
    jwks.Keyfunc,
    jwt.WithValidMethods([]string{"RS256"}),
    jwt.WithIssuer("https://YOUR_DOMAIN/"),
)))
```

A token that fails verification (bad signature, expired, wrong algorithm) gives **no** user, so the call is treated as having no user. See [Calls with no user](#calls-with-no-user) below.

### Option C: set the user in your code

Outside HTTP handlers, such as in background jobs, queue consumers or CLIs, set the user directly. It's verified because your code chose it:

```go
otellix.Trace(ctx, provider, params, otellix.WithUserID(job.OwnerID), otellix.WithBudgetConfig(budget))
```

---

## Setting up verified projects

A project, team or tenant ID should come from your own records, typically the organisation of the signed-in user, never from something the caller sends:

```go
r.Use(func(c *gin.Context) {
    user := currentUser(c)          // from your verified session
    c.Set("user_id", user.ID)
    c.Set("project_id", user.OrgID) // from your database, not a header
    c.Next()
})

r.Use(otellixmw.GinMiddleware(
    otellixmw.WithUserFromContext("user_id"),
    otellixmw.WithProjectFromContext("project_id"),
))
```

If a user can belong to several projects and chooses one per request, for example with a header, check their membership in your auth middleware before storing the project ID.

`WithProjectHeader("X-Project-ID")` still exists for attribution without budgets. The ID it reads is unverified, so a `PerProjectLimit` on it is refused.

---

## Calls with no user

A call with no user ID at all (no token, or one that failed verification) isn't covered by `PerUserLimit`. That's unchanged. Either:

- make sure your routes reject requests without a valid token before any LLM call runs, or
- set `BudgetConfig.UnattributedLimit` to cap all calls with no user together.

Otherwise, a caller who sends no token, or a bad one, isn't held to any per-user limit. See [Cost Attribution](attribution.md#calls-with-no-user).

---

## Writing your own middleware

If you attribute requests in your own middleware, use the matching context function so budgets know how far to trust the ID:

```go
// From your verified session: budgets can apply limits to it.
ctx = otellix.ContextWithUser(ctx, session.UserID)

// From something the caller controls: attributed, but never limited.
ctx = otellix.ContextWithUnverifiedUser(ctx, r.Header.Get("X-Client-User"))
```

---

## What's recorded on spans

| Attribute | On | Meaning |
| --- | --- | --- |
| `llm.user_verified` | `llm.call` and `http.request` spans with a user | `true` if the user ID was verified |
| `llm.project_verified` | `llm.call` and `http.request` spans with a project | `true` if the project ID was verified |

To find unverified attribution in your tracing backend, filter on `llm.user_verified = false`.

---

## Upgrading from v1.7 or earlier

Before v1.8.0, the middleware read the user from an unverified JWT and the project from the `X-Project-ID` header, and budgets applied limits to both. Anyone could pick their own IDs.

After upgrading:

1. **If you use budgets with the middleware's defaults,** budgeted calls return `UnverifiedIdentityError`. Switch to [Option A](#option-a-read-the-user-from-your-auth-middleware-recommended) or [Option B](#option-b-have-otellix-verify-the-jwt).
2. **If you relied on `X-Project-ID`,** the project is no longer read. Use `WithProjectFromContext` with a project from your auth middleware. To keep reading the header for attribution only, add `WithProjectHeader("X-Project-ID")`.
3. **If you use the Fiber middleware,** upgrade `github.com/oluwajubelo1/otellix/middleware/fiber` to v0.2.0 or later together with Otellix v1.8.0. Earlier Fiber versions can't tell Otellix that an ID came from your auth, so it treats every ID from them as unverified.
4. **If you set IDs in your own code** with `WithUserID`, `WithProjectID`, `ContextWithUser` or `ContextWithProject`, nothing changes.

---

## Troubleshooting

**Every budgeted call fails with `UnverifiedIdentityError`.**
The middleware is reading an unverified ID. Check its options: you need `WithUserFromContext` or `WithVerifiedJWT` for users, and `WithProjectFromContext` for projects. Look at `llm.user_verified` and `llm.project_verified` on the `http.request` span.

**I use `WithUserFromContext` but calls are still refused.**
- Check the Otellix middleware is registered after your auth middleware.
- On Fiber, check you're on `middleware/fiber` v0.2.0 or later.
- Check nothing else in your code calls `ContextWithUnverifiedUser`.

**`WithVerifiedJWT` gives no user for valid-looking tokens.**
The token failed verification. Check the key, that `jwt.WithValidMethods` lists the algorithm the token is signed with, and that the token hasn't expired. Issuer and audience options must match the token's claims exactly.

**Calls from a background job are refused.**
The job's context probably carries an unverified ID from the request that queued it. Set the user explicitly with `otellix.WithUserID`, which takes precedence.
