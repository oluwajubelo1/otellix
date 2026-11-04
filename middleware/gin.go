// Package middleware provides HTTP middleware for automatic LLM call attribution.
//
// gin.go extracts user_id and project_id from requests and injects them into the
// context so downstream otellix.Trace() calls pick them up automatically.
//
// Budgets only apply limits to verified identity. With no options, the user
// comes from the JWT's "sub" claim without checking its signature, so calls
// are attributed in traces and metrics, but a budget limit on that user
// returns otellix.UnverifiedIdentityError. Verify it one of two ways.
//
// Read identity your auth middleware has already verified:
//
//	r.Use(authMiddleware) // sets c.Set("user_id", ...) after verifying the token
//	r.Use(otellixmw.GinMiddleware(otellixmw.WithUserFromContext("user_id")))
//
// Or have the middleware verify the JWT itself:
//
//	r.Use(otellixmw.GinMiddleware(otellixmw.WithVerifiedJWT(
//	    func(*jwt.Token) (any, error) { return secret, nil },
//	    jwt.WithValidMethods([]string{"HS256"}),
//	)))
//
// The project is only read from verified identity, WithProjectFromContext,
// unless you opt in to a header with WithProjectHeader.
package middleware

import (
	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/oluwajubelo1/otellix/internal/identity"
)

// MiddlewareConfig holds configuration for HTTP middleware.
type MiddlewareConfig = identity.Config

// MiddlewareOption is a functional option for middleware configuration.
type MiddlewareOption func(*MiddlewareConfig)

// WithJWTClaim sets the JWT claim name for user_id extraction. Default: "sub".
func WithJWTClaim(claim string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.JWTClaim = claim }
}

// WithVerifiedJWT verifies the JWT in the Authorization header before
// reading user_id from it: its signature with keyFunc, and its expiry. A
// token that fails verification gives no user_id. The user_id is then
// verified, so budgets apply per-user limits to it. Pass parser options
// such as jwt.WithValidMethods to pin the signing algorithm.
func WithVerifiedJWT(keyFunc jwt.Keyfunc, opts ...jwt.ParserOption) MiddlewareOption {
	return func(c *MiddlewareConfig) {
		c.JWTKeyFunc = keyFunc
		c.JWTParserOptions = opts
	}
}

// WithProjectHeader reads project_id from the given HTTP header. Default:
// no header is read. Callers can set a header to anything, so the
// project_id is unverified: calls are attributed to it, but budgets won't
// apply a per-project limit to it. Use WithProjectFromContext for a project
// your auth middleware has verified.
func WithProjectHeader(header string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.ProjectHeader = header }
}

// WithUserFromContext reads user_id from the framework context value under key
// (gin.Context.Get / echo.Context.Get) instead of parsing the JWT. Use it
// with an auth middleware that verifies the caller and stores their ID, and
// register this middleware after it. The value must be a string. The
// user_id is treated as verified, so budgets apply per-user limits to it.
func WithUserFromContext(key string) MiddlewareOption {
	return func(c *MiddlewareConfig) { c.UserContextKey = key }
}

// WithProjectFromContext reads project_id from the framework context value
// under key, so callers cannot choose which project's budget they spend.
// The value must be a string. The project_id is treated as verified, so
// budgets apply per-project limits to it.
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
		ctx = cfg.Attribute(ctx, span, userID, projectID)

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
