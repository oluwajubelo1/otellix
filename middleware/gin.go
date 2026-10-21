// Package middleware provides HTTP middleware for automatic LLM call attribution.
//
// gin.go extracts user_id and project_id from requests and injects them into the
// context so downstream otellix.Trace() calls pick them up automatically.
//
// Usage:
//
//	r := gin.Default()
//	r.Use(otellixmw.GinMiddleware())
//
// With custom config:
//
//	r.Use(otellixmw.GinMiddleware(
//	    otellixmw.WithJWTClaim("user_id"),
//	    otellixmw.WithProjectHeader("X-Tenant-ID"),
//	))
//
// Reading identity your auth middleware has already verified (recommended
// whenever budgets are enforced):
//
//	r.Use(authMiddleware) // sets c.Set("user_id", ...) after verifying the token
//	r.Use(otellixmw.GinMiddleware(otellixmw.WithUserFromContext("user_id")))
package middleware

import (
	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/oluwajubelo1/otellix/internal/identity"
)

// MiddlewareConfig holds configuration for HTTP middleware.
type MiddlewareConfig = identity.Config

// MiddlewareOption is a functional option for middleware configuration.
type MiddlewareOption func(*MiddlewareConfig)

// WithJWTClaim sets the JWT claim name for user_id extraction.
func WithJWTClaim(claim string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.JWTClaim = claim }
}

// WithProjectHeader sets the HTTP header name for project_id extraction.
func WithProjectHeader(header string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.ProjectHeader = header }
}

// WithUserFromContext reads user_id from the framework context value under key
// (gin.Context.Get / echo.Context.Get) instead of parsing the JWT. Use it
// with an auth middleware that verifies the caller and stores their ID, and
// register this middleware after it. The value must be a string.
//
// The default JWT extraction does not verify signatures, so on routes without
// verified auth a caller can choose any user_id, including a new one per
// request to sidestep per-user budgets.
func WithUserFromContext(key string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.UserContextKey = key }
}

// WithProjectFromContext reads project_id from the framework context value
// under key instead of the project header, so callers cannot choose which
// project's budget they spend. The value must be a string.
func WithProjectFromContext(key string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.ProjectContextKey = key }
}

// GinMiddleware returns a Gin middleware that extracts user_id and project_id
// from requests and injects them into the context for downstream Trace() calls.
func GinMiddleware(opts ...MiddlewareOption) gin.HandlerFunc {
	cfg := identity.Default()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		tracer := otel.Tracer(identity.TracerName)
		ctx, span := tracer.Start(c.Request.Context(), "http.request",
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		userID, projectID := cfg.Resolve(func(key string) any {
			v, _ := c.Get(key)
			return v
		}, c.GetHeader)
		ctx = identity.Apply(ctx, span, userID, projectID)

		// Set request attributes.
		span.SetAttributes(
			attribute.String("http.method", c.Request.Method),
			attribute.String("http.route", c.FullPath()),
		)

		c.Request = c.Request.WithContext(ctx)
		c.Next()

		// Set response status.
		span.SetAttributes(attribute.Int("http.status_code", c.Writer.Status()))
	}
}
