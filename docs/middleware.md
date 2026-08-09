# Middleware & Identity Discovery

Tracking costs is only useful if you know **who** is spending the money. Otellix provides HTTP middleware to automatically extract identity from incoming requests.

## Gin Middleware

The Gin middleware extracts `user_id` from the `Authorization: Bearer <jwt>` header (JWT `sub` claim by default) and `project_id` from the `X-Project-ID` header, then populates the `context.Context` so downstream `otellix.Trace()` calls pick them up automatically.

### Setup

```go
import (
    "github.com/gin-gonic/gin"
    otellixmw "github.com/oluwajubelo1/otellix/middleware"
)

func main() {
    r := gin.Default()

    // Global middleware
    r.Use(otellixmw.GinMiddleware())

    r.POST("/generate", func(c *gin.Context) {
        // Otellix automatically finds the user from the context!
        res, err := otellix.Trace(c.Request.Context(), provider, params)
    })
}
```

### Configuration
By default, the middleware looks for:
- `user_id` — from the JWT `sub` claim in the `Authorization: Bearer ...` header
- `project_id` — from the `X-Project-ID` header

You can customize this behavior:
```go
r.Use(otellixmw.GinMiddleware(
    otellixmw.WithJWTClaim("user_id"),
    otellixmw.WithProjectHeader("X-Tenant-ID"),
))
```

> Note: JWT parsing is **unverified** — the middleware only extracts the claim. Authentication is the responsibility of your application's auth middleware.

## Echo Middleware

Similarly, for the Echo framework:

```go
import (
    "github.com/labstack/echo/v4"
    otellixmw "github.com/oluwajubelo1/otellix/middleware"
)

e := echo.New()
e.Use(otellixmw.EchoMiddleware())
```

## Why use Middleware?
Using middleware ensures that your LLM traces are **consistently attributed**. Even nested functions that take a `context.Context` will have access to the identity information without you having to pass it manually through every function signature.
