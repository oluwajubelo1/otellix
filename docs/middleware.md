# Middleware & Identity Discovery

Tracking costs is only useful if you know **who** is spending the money. Otellix provides HTTP middleware to automatically extract identity from incoming requests.

## Gin Middleware

The Gin middleware looks for standard headers and populates the `context.Context` with `user_id` and `project_id`.

### Setup

```go
import (
    "github.com/gin-gonic/gin"
    otellixgin "github.com/oluwajubelo1/otellix/middleware/gin"
)

func main() {
    r := gin.Default()
    
    // Global middleware
    r.Use(otellixgin.Middleware())

    r.POST("/generate", func(c *gin.Context) {
        // Otellix automatically finds the user from the context!
        res, err := otellix.Trace(c.Request.Context(), provider, params)
    })
}
```

### Configuration
By default, the middleware looks for:
- `X-User-ID`
- `X-Project-ID`

You can customize this behavior:
```go
r.Use(otellixgin.Middleware(
    otellixgin.WithUserHeader("X-Custom-User"),
    otellixgin.WithProjectHeader("X-Custom-Project"),
))
```

## Echo Middleware

Similarly, for the Echo framework:

```go
import (
    "github.com/labstack/echo/v4"
    otellixecho "github.com/oluwajubelo1/otellix/middleware/echo"
)

e := echo.New()
e.Use(otellixecho.Middleware())
```

## Why use Middleware?
Using middleware ensures that your LLM traces are **consistently attributed**. Even nested functions that take a `context.Context` will have access to the identity information without you having to pass it manually through every function signature.
