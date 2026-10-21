// Package identity holds the request-attribution logic shared by the Gin,
// Echo and Fiber middleware, so each framework adapter stays a thin wrapper.
package identity

import (
	"context"
	"strings"

	jwt "github.com/golang-jwt/jwt/v5"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/oluwajubelo1/otellix"
)

// TracerName is the instrumentation name for request spans started by the middleware.
const TracerName = "github.com/oluwajubelo1/otellix/middleware"

// Config holds configuration for HTTP middleware.
type Config struct {
	// JWTClaim is the JWT claim name to extract user_id from. Default: "sub".
	JWTClaim string

	// ProjectHeader is the HTTP header name to extract project_id from. Default: "X-Project-ID".
	ProjectHeader string

	// UserContextKey, when set, reads user_id from the framework context
	// value under this key instead of parsing the JWT.
	UserContextKey string

	// ProjectContextKey, when set, reads project_id from the framework
	// context value under this key instead of the project header.
	ProjectContextKey string
}

// Default returns the default middleware configuration.
func Default() *Config {
	return &Config{
		JWTClaim:      "sub",
		ProjectHeader: "X-Project-ID",
	}
}

// Resolve returns user_id and project_id for a request. get reads a
// framework context value; header reads a request header.
func (c *Config) Resolve(get func(key string) any, header func(name string) string) (userID, projectID string) {
	if c.UserContextKey != "" {
		userID, _ = get(c.UserContextKey).(string)
	} else {
		userID = UserFromJWT(header("Authorization"), c.JWTClaim)
	}

	if c.ProjectContextKey != "" {
		projectID, _ = get(c.ProjectContextKey).(string)
	} else {
		projectID = header(c.ProjectHeader)
	}
	return userID, projectID
}

// Apply records the identity on the request span and in ctx for downstream Trace() calls.
func Apply(ctx context.Context, span trace.Span, userID, projectID string) context.Context {
	if userID != "" {
		span.SetAttributes(attribute.String("llm.user_id", userID))
		ctx = otellix.ContextWithUser(ctx, userID)
	}
	if projectID != "" {
		span.SetAttributes(attribute.String("llm.project_id", projectID))
		ctx = otellix.ContextWithProject(ctx, projectID)
	}
	span.SetAttributes(attribute.Bool("llm.user_attributed", userID != ""))
	return ctx
}

// UserFromJWT parses a JWT from the Authorization header and extracts the
// specified claim. Uses unverified parsing — verification is the
// responsibility of the application's auth middleware.
func UserFromJWT(authHeader, claimName string) string {
	if authHeader == "" {
		return ""
	}

	// Strip "Bearer " prefix.
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return "" // no "Bearer " prefix found
	}

	// Parse without verification — auth validation is the app's responsibility.
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
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
