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
package middleware

import (
	"context"
	"strings"

	"github.com/gin-gonic/gin"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/golang-jwt/jwt/v5"
)

// Context keys for user/project extraction.
type contextKey string

const (
	userIDKey    contextKey = "otellix.user_id"
	projectIDKey contextKey = "otellix.project_id"
)

// MiddlewareConfig holds configuration for HTTP middleware.
type MiddlewareConfig struct {
	// JWTClaim is the JWT claim name to extract user_id from. Default: "sub".
	JWTClaim string

	// ProjectHeader is the HTTP header name to extract project_id from. Default: "X-Project-ID".
	ProjectHeader string
}

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

func defaultMiddlewareConfig() *MiddlewareConfig {
	return &MiddlewareConfig{
		JWTClaim:      "sub",
		ProjectHeader: "X-Project-ID",
	}
}

// GinMiddleware returns a Gin middleware that extracts user_id and project_id
// from requests and injects them into the context for downstream Trace() calls.
func GinMiddleware(opts ...MiddlewareOption) gin.HandlerFunc {
	cfg := defaultMiddlewareConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c *gin.Context) {
		tracer := otel.Tracer("github.com/oluwajubelo1/otellix/middleware")
		ctx, span := tracer.Start(c.Request.Context(), "http.request",
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		// Extract user_id from JWT.
		userID := extractUserIDFromJWT(c.GetHeader("Authorization"), cfg.JWTClaim)
		if userID != "" {
			span.SetAttributes(attribute.String("llm.user_id", userID))
			ctx = context.WithValue(ctx, userIDKey, userID)
		}

		// Extract project_id from header.
		projectID := c.GetHeader(cfg.ProjectHeader)
		if projectID != "" {
			span.SetAttributes(attribute.String("llm.project_id", projectID))
			ctx = context.WithValue(ctx, projectIDKey, projectID)
		}

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

// UserIDFromContext returns the user_id injected by the middleware.
func UserIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(userIDKey).(string); ok {
		return v
	}
	return ""
}

// ProjectIDFromContext returns the project_id injected by the middleware.
func ProjectIDFromContext(ctx context.Context) string {
	if v, ok := ctx.Value(projectIDKey).(string); ok {
		return v
	}
	return ""
}

// extractUserIDFromJWT parses a JWT from the Authorization header and extracts
// the specified claim. Uses unverified parsing — verification is the responsibility
// of the application's auth middleware.
func extractUserIDFromJWT(authHeader, claimName string) string {
	if authHeader == "" {
		return ""
	}

	// Strip "Bearer " prefix.
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return "" // no "Bearer " prefix found
	}

	// Parse without verification — auth validation is the app's responsibility.
	parser := jwt.NewParser()
	token, _, err := parser.ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return ""
	}

	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}

	if val, ok := claims[claimName].(string); ok {
		return val
	}
	return ""
}
