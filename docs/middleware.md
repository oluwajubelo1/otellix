# Middleware & Identity

Tracking costs is only useful if you know **who** is spending the money. Otellix provides HTTP middleware for **Gin**, **Echo** and **Fiber** that reads the user and project from each incoming request and puts them on the request context. Every `otellix.Trace()` and `otellix.TraceStream()` call made while handling that request is then attributed automatically. You don't have to pass IDs through your code.

> This guide covers the HTTP side. For how attribution works in general (precedence, metrics, finding calls with no user, `UnattributedLimit`), see [Cost Attribution](attribution.md). **If you enforce budgets, also read [Verified Identity & Budgets](verified-identity.md):** budgets only apply limits to IDs the middleware has verified.

---

## How it works

For every request, the middleware:

1. Starts an `http.request` server span.
2. Works out the `user_id` and `project_id` (see [Where identity comes from](#where-identity-comes-from)).
3. Stores them on the request context, marked as verified or unverified (see [Verified Identity & Budgets](verified-identity.md)), and records them on the span.
4. Runs your handler.
5. Records the HTTP method, route and status code on the span.

```text
request ─▶ [your auth middleware] ─▶ [otellix middleware] ─▶ handler ─▶ otellix.Trace(ctx, ...)
                                       sets user_id / project_id     reads them from ctx
```

## Where identity comes from

By default:

| Identifier | Source | Verified? |
| --- | --- | --- |
| `user_id` | The `sub` claim of the JWT in `Authorization: Bearer <token>`, read **without** checking its signature | No |
| `project_id` | Not read | n/a |

If the claim is missing, the user is left empty and the call is [unattributed](attribution.md#calls-with-no-user).

> **The default doesn't verify the JWT.** It decodes the token and reads the claim, but never checks the signature, so anyone can make a token with any `sub`. That's fine for tracing, so calls are still attributed. But **budgets won't apply a per-user limit to an unverified user**: a budgeted call returns `otellix.UnverifiedIdentityError` instead. To enforce budgets, verify the user with [`WithUserFromContext` or `WithVerifiedJWT`](#use-verified-identity-when-you-enforce-budgets).

> **Changed in v1.8.0:** the `X-Project-ID` header is no longer read by default, because callers can set it to any project. Read the project from your auth middleware with `WithProjectFromContext`, or opt back in to the header, unverified, with `WithProjectHeader("X-Project-ID")`.

---

## Gin

### Install

The Gin middleware is part of the main module:

```bash
go get github.com/oluwajubelo1/otellix
```

### Basic setup

```go
package main

import (
    "net/http"

    "github.com/gin-gonic/gin"

    "github.com/oluwajubelo1/otellix"
    otellixmw "github.com/oluwajubelo1/otellix/middleware"
    "github.com/oluwajubelo1/otellix/providers"
    "github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
    shutdown := otellix.SetupDevConsole() // readable summaries in your terminal
    defer shutdown()

    provider := openai.New()

    r := gin.Default()
    r.Use(otellixmw.GinMiddleware())

    r.POST("/generate", func(c *gin.Context) {
        // Pass the REQUEST's context: that's where the middleware stored the identity.
        res, err := otellix.Trace(c.Request.Context(), provider, providers.CallParams{
            Model:    "gpt-4o-mini",
            Messages: []providers.Message{{Role: "user", Content: c.PostForm("prompt")}},
        })
        if err != nil {
            c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
            return
        }
        // res.RawResponse holds the provider SDK's full response, including the text.
        c.JSON(http.StatusOK, gin.H{"output_tokens": res.OutputTokens})
    })

    r.Run(":8080")
}
```

Try it:

```bash
# A test token with sub=user_123 (not signed with a real key, fine for local testing)
TOKEN="eyJhbGciOiJIUzI1NiJ9.eyJzdWIiOiJ1c2VyXzEyMyJ9.x"

curl -X POST localhost:8080/generate \
  -H "Authorization: Bearer $TOKEN" \
  -d "prompt=Say hello"
```

The console summary for the call shows `user: user_123`. This setup attributes calls for tracing. To enforce budgets, verify the identity as below.

### Use verified identity

**From your auth middleware.** Register your auth middleware first. Have it call `c.Set` with the verified user ID, then point Otellix at that key:

```go
r := gin.Default()

r.Use(func(c *gin.Context) {
    claims, err := verifyToken(c.GetHeader("Authorization")) // your real verification
    if err != nil {
        c.AbortWithStatus(http.StatusUnauthorized)
        return
    }
    c.Set("user_id", claims.Subject)
    c.Set("project_id", claims.OrgID)
    c.Next()
})

r.Use(otellixmw.GinMiddleware(
    otellixmw.WithUserFromContext("user_id"),
    otellixmw.WithProjectFromContext("project_id"),
))
```

**Or have Otellix verify the JWT.** Give it the key your tokens are signed with:

```go
import jwt "github.com/golang-jwt/jwt/v5"

r.Use(otellixmw.GinMiddleware(otellixmw.WithVerifiedJWT(
    func(*jwt.Token) (any, error) { return []byte(os.Getenv("JWT_SECRET")), nil },
    jwt.WithValidMethods([]string{"HS256"}),
)))
```

For tokens from an identity provider with a JWKS URL, see [Verified Identity & Budgets](verified-identity.md#option-b-have-otellix-verify-the-jwt).

---

## Echo

### Install

The Echo middleware is also part of the main module:

```bash
go get github.com/oluwajubelo1/otellix
```

### Basic setup

```go
import (
    "net/http"

    "github.com/labstack/echo/v4"

    "github.com/oluwajubelo1/otellix"
    otellixmw "github.com/oluwajubelo1/otellix/middleware"
    "github.com/oluwajubelo1/otellix/providers"
)

e := echo.New()
e.Use(otellixmw.EchoMiddleware())

e.POST("/generate", func(c echo.Context) error {
    res, err := otellix.Trace(c.Request().Context(), provider, providers.CallParams{
        Model:    "gpt-4o-mini",
        Messages: []providers.Message{{Role: "user", Content: c.FormValue("prompt")}},
    })
    if err != nil {
        return err
    }
    return c.JSON(http.StatusOK, map[string]int64{"output_tokens": res.OutputTokens})
})

e.Logger.Fatal(e.Start(":8080"))
```

### Use verified identity

Echo's equivalent of `c.Set` is the same method on `echo.Context`:

```go
e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
    return func(c echo.Context) error {
        claims, err := verifyToken(c.Request().Header.Get("Authorization"))
        if err != nil {
            return echo.ErrUnauthorized
        }
        c.Set("user_id", claims.Subject)
        c.Set("project_id", claims.OrgID)
        return next(c)
    }
})

e.Use(otellixmw.EchoMiddleware(
    otellixmw.WithUserFromContext("user_id"),
    otellixmw.WithProjectFromContext("project_id"),
))
```

If you use Echo's JWT middleware (`echojwt`), it stores the parsed token rather than a string, so add a small middleware after it that copies the subject out with `c.Set("user_id", subject)`. Or use `otellixmw.WithVerifiedJWT(...)`, exactly as for Gin.

---

## Fiber

Fiber **v3** is supported. Fiber v2 is not; open an issue if you need it.

### Install

The Fiber middleware is a **separate Go module**, so Otellix users who don't use Fiber never download it:

```bash
go get github.com/oluwajubelo1/otellix/middleware/fiber
```

It needs **Go 1.25 or newer**, the same as Fiber v3 itself. The main Otellix module still supports Go 1.23.

> **Use `middleware/fiber` v0.2.0 or later with Otellix v1.8.0 or later.** Earlier Fiber versions can't tell Otellix that an ID came from your auth middleware, so budgets treat every ID from them as unverified and refuse budgeted calls. `WithVerifiedJWT` is also new in v0.2.0.

### Basic setup

```go
package main

import (
    "github.com/gofiber/fiber/v3"

    "github.com/oluwajubelo1/otellix"
    otellixfiber "github.com/oluwajubelo1/otellix/middleware/fiber"
    "github.com/oluwajubelo1/otellix/providers"
    "github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
    shutdown := otellix.SetupDevConsole()
    defer shutdown()

    provider := openai.New()

    app := fiber.New()
    app.Use(otellixfiber.Middleware())

    app.Post("/generate", func(c fiber.Ctx) error {
        // Pass c.Context(), NOT c. See the warning below.
        res, err := otellix.Trace(c.Context(), provider, providers.CallParams{
            Model:    "gpt-4o-mini",
            Messages: []providers.Message{{Role: "user", Content: c.FormValue("prompt")}},
        })
        if err != nil {
            return err
        }
        return c.JSON(fiber.Map{"output_tokens": res.OutputTokens})
    })

    app.Listen(":8080")
}
```

> **Pass `c.Context()`, not `c`.** In Fiber v3, `fiber.Ctx` itself satisfies `context.Context`, so `otellix.Trace(c, ...)` compiles. But it reads values from Fiber's own request store, not from the context the middleware set. The call would be unattributed, and refused by a budget with per-user limits. Always pass `c.Context()`.

### Use verified identity

Fiber's request-scoped store is `c.Locals`. Have your auth middleware set the verified IDs there:

```go
app.Use(func(c fiber.Ctx) error {
    claims, err := verifyToken(c.Get("Authorization"))
    if err != nil {
        return fiber.ErrUnauthorized
    }
    c.Locals("user_id", claims.Subject)
    c.Locals("project_id", claims.OrgID)
    return c.Next()
})

app.Use(otellixfiber.Middleware(
    otellixfiber.WithUserFromContext("user_id"),
    otellixfiber.WithProjectFromContext("project_id"),
))
```

Or verify the JWT with `otellixfiber.WithVerifiedJWT(keyFunc, opts...)`, as for Gin.

### Fiber-specific behaviour

- `http.route` is the matched route pattern (e.g. `/users/:id`), recorded after your handler runs. That's the point at which Fiber knows the final route.
- If your handler returns an error, `http.status_code` is taken from it: a `*fiber.Error` gives its code (e.g. `fiber.ErrNotFound` gives 404), and any other error gives 500. That's the status Fiber's error handler sends.

---

## Options reference

All three frameworks take the same options. Gin and Echo use `otellixmw.With...` from `github.com/oluwajubelo1/otellix/middleware`; Fiber uses `otellixfiber.With...` from `github.com/oluwajubelo1/otellix/middleware/fiber`.

| Option | Default | What it does |
| --- | --- | --- |
| `WithJWTClaim(name)` | `"sub"` | Which JWT claim holds the user ID. The claim must be a string. |
| `WithVerifiedJWT(keyFunc, opts...)` | not set | Verify the JWT's signature with `keyFunc`, and its expiry, before reading the claim. The user ID is then **verified**. A token that fails verification gives no user. `opts` are `jwt.ParserOption`s, such as `jwt.WithValidMethods`. |
| `WithUserFromContext(key)` | not set | Read the user ID from the framework's request store instead of the JWT. Gin: `c.Get(key)`. Echo: `c.Get(key)`. Fiber: `c.Locals(key)`. When set, the `Authorization` header is ignored. The user ID is **verified**. |
| `WithProjectFromContext(key)` | not set | Read the project ID from the framework's request store. When set, any project header is ignored. The project ID is **verified**. |
| `WithProjectHeader(name)` | not set | Read the project ID from a request header. Callers can set headers to anything, so the project ID is **unverified**: attributed, but never limited. |

Values read with the context options must be **strings**. Anything else (a number, a struct, a pointer) is treated as missing.

Example: a verified JWT that keeps the user in `user_id`, and the tenant from your auth middleware:

```go
r.Use(otellixmw.GinMiddleware(
    otellixmw.WithVerifiedJWT(keyFunc, jwt.WithValidMethods([]string{"RS256"})),
    otellixmw.WithJWTClaim("user_id"),
    otellixmw.WithProjectFromContext("tenant_id"),
))
```

---

## Use verified identity when you enforce budgets

Budgets only apply limits to verified IDs. Anything a caller sends can be forged:

- **An unverified JWT.** A caller can make a token with any `sub`, including a new one on every request, to get a fresh per-user budget each time.
- **A header.** A caller can set a project header to someone else's project and spend that project's budget.

So if a budget limit would depend on an ID read from either, Otellix refuses the call with `otellix.UnverifiedIdentityError` instead of enforcing a limit that could be dodged. The provider isn't called.

To enforce budgets, use one of:

1. **`WithUserFromContext` / `WithProjectFromContext`:** verify the caller in your auth middleware, store the IDs in the framework's request store as strings, and register the Otellix middleware **after** it. If the Otellix middleware runs first, the auth middleware hasn't stored anything yet, and every call is unattributed.
2. **`WithVerifiedJWT`:** Otellix checks the token's signature and expiry itself.

With either, a forged token or header has no effect on attribution or budgets.

[Verified Identity & Budgets](verified-identity.md) has the full rules, JWKS setup, and an upgrade checklist.

---

## What gets recorded on the request span

| Attribute | When |
| --- | --- |
| `llm.user_id` | A user ID was found |
| `llm.user_verified` | A user ID was found: `true` if it was verified |
| `llm.project_id` | A project ID was found |
| `llm.project_verified` | A project ID was found: `true` if it was verified |
| `llm.user_attributed` | Always: `true` or `false` |
| `http.method` | Always |
| `http.route` | Always: the route pattern, e.g. `/users/:id` |
| `http.status_code` | Always, after the handler finishes |

The LLM spans created inside the request are children of this span, so in your tracing backend you see the HTTP request and every LLM call it made in one trace.

---

## Calls with no user

Requests with no user ID can't be held to `PerUserLimit`, so a budget that sets it refuses them with `otellix.UnattributedCallError`, unless `BudgetConfig.UnattributedLimit` caps them. That includes requests without a token, and with `WithVerifiedJWT`, requests whose token failed verification. To find them, filter spans on `llm.user_attributed = false`. Both are covered in detail in [Cost Attribution](attribution.md#calls-with-no-user).

---

## Troubleshooting

**Calls show an empty `user_id`.**
- Check that you pass the request's context: `c.Request.Context()` (Gin), `c.Request().Context()` (Echo), `c.Context()` (Fiber).
- With the default settings: check the header is exactly `Authorization: Bearer <token>`, and that the claim (default `sub`) exists and is a string.
- With `WithVerifiedJWT`: a token that fails verification gives no user. Check the key, the signing algorithm, and that the token hasn't expired.
- With `WithUserFromContext`: check the Otellix middleware is registered *after* your auth middleware, and that the stored value is a `string`.

**Budgeted calls fail with `UnverifiedIdentityError`.**
The middleware read the user or project from an unverified source. Use `WithUserFromContext` or `WithVerifiedJWT` for users, and `WithProjectFromContext` for projects. See [Verified Identity & Budgets](verified-identity.md#troubleshooting).

**Calls lost their project after upgrading.**
From v1.8.0, `X-Project-ID` is no longer read by default. Use `WithProjectFromContext`, or `WithProjectHeader("X-Project-ID")` for attribution without budgets.

**Fiber calls are never attributed.**
You're probably passing `c` instead of `c.Context()` to `otellix.Trace`.

**Streaming calls aren't attributed.**
`TraceStream` only reads identity from the context from this release on. Upgrade Otellix.

**Background jobs aren't attributed.**
They don't run inside an HTTP request, so no middleware sees them. Attribute them in code with `otellix.WithUserID(...)`. See [Cost Attribution](attribution.md#step-2-fix-what-you-can).

---

## Why use middleware?

Middleware makes attribution **consistent**. Every LLM call made while handling a request is attributed the same way, including calls deep inside helper functions that only receive a `context.Context`. You don't have to remember to pass IDs at each call site.
