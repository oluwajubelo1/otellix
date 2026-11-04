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

	// JWTKeyFunc, when set, verifies the JWT's signature and expiry with this
	// key function before reading the claim, so budgets can trust the user
	// ID. Without it, the JWT is read unverified.
	JWTKeyFunc jwt.Keyfunc

	// JWTParserOptions are passed to the JWT parser when verifying, e.g.
	// jwt.WithValidMethods([]string{"HS256"}).
	JWTParserOptions []jwt.ParserOption

	// ProjectHeader is the HTTP header name to extract project_id from.
	// Default: none. A header is unverified, since callers can set it.
	ProjectHeader string

	// UserContextKey, when set, reads user_id from the framework context
	// value under this key instead of parsing the JWT.
	UserContextKey string

	// ProjectContextKey, when set, reads project_id from the framework
	// context value under this key instead of the project header.
	ProjectContextKey string
}

// Default returns the default middleware configuration: the user from the
// JWT's "sub" claim, unverified, and no project.
func Default() *Config {
	return &Config{
		JWTClaim: "sub",
	}
}

// Resolve returns user_id and project_id for a request. get reads a
// framework context value; header reads a request header.
func (c *Config) Resolve(get func(key string) any, header func(name string) string) (userID, projectID string) {
	switch {
	case c.UserContextKey != "":
		userID, _ = get(c.UserContextKey).(string)
	case c.JWTKeyFunc != nil:
		userID = UserFromVerifiedJWT(header("Authorization"), c.JWTClaim, c.JWTKeyFunc, c.JWTParserOptions...)
	default:
		userID = UserFromJWT(header("Authorization"), c.JWTClaim)
	}

	switch {
	case c.ProjectContextKey != "":
		projectID, _ = get(c.ProjectContextKey).(string)
	case c.ProjectHeader != "":
		projectID = header(c.ProjectHeader)
	}
	return userID, projectID
}

// Verified reports whether the user and project IDs Resolve returns can be
// trusted: they come from the app's own auth, or from a verified JWT.
func (c *Config) Verified() (user, project bool) {
	return c.UserContextKey != "" || c.JWTKeyFunc != nil, c.ProjectContextKey != ""
}

// Attribute records the identity on the request span and in ctx for
// downstream Trace() calls, marking IDs that weren't verified so budgets
// won't apply limits to them.
func (c *Config) Attribute(ctx context.Context, span trace.Span, userID, projectID string) context.Context {
	userVerified, projectVerified := c.Verified()
	return record(ctx, span, userID, projectID, userVerified, projectVerified)
}

// Apply records the identity on the request span and in ctx, as unverified.
//
// Deprecated: Apply can't tell where the IDs came from, so it treats them as
// unverified. It is kept for middleware built against earlier versions; use
// Config.Attribute.
func Apply(ctx context.Context, span trace.Span, userID, projectID string) context.Context {
	return record(ctx, span, userID, projectID, false, false)
}

// record sets the identity on span and in ctx, as verified or not.
func record(ctx context.Context, span trace.Span, userID, projectID string, userVerified, projectVerified bool) context.Context {
	if userID != "" {
		span.SetAttributes(
			attribute.String("llm.user_id", userID),
			attribute.Bool("llm.user_verified", userVerified),
		)
		if userVerified {
			ctx = otellix.ContextWithUser(ctx, userID)
		} else {
			ctx = otellix.ContextWithUnverifiedUser(ctx, userID)
		}
	}
	if projectID != "" {
		span.SetAttributes(
			attribute.String("llm.project_id", projectID),
			attribute.Bool("llm.project_verified", projectVerified),
		)
		if projectVerified {
			ctx = otellix.ContextWithProject(ctx, projectID)
		} else {
			ctx = otellix.ContextWithUnverifiedProject(ctx, projectID)
		}
	}
	span.SetAttributes(attribute.Bool("llm.user_attributed", userID != ""))
	return ctx
}

// UserFromJWT parses a JWT from the Authorization header and extracts the
// specified claim. Uses unverified parsing — verification is the
// responsibility of the application's auth middleware, or of
// UserFromVerifiedJWT.
func UserFromJWT(authHeader, claimName string) string {
	tokenStr := bearerToken(authHeader)
	if tokenStr == "" {
		return ""
	}

	// Parse without verification — auth validation is the app's responsibility.
	token, _, err := jwt.NewParser().ParseUnverified(tokenStr, jwt.MapClaims{})
	if err != nil {
		return ""
	}
	return claim(token, claimName)
}

// UserFromVerifiedJWT parses a JWT from the Authorization header, verifies
// its signature with keyFunc and its expiry, and extracts the specified
// claim. It returns "" for a token that fails verification.
func UserFromVerifiedJWT(authHeader, claimName string, keyFunc jwt.Keyfunc, opts ...jwt.ParserOption) string {
	tokenStr := bearerToken(authHeader)
	if tokenStr == "" {
		return ""
	}

	token, err := jwt.NewParser(opts...).Parse(tokenStr, keyFunc)
	if err != nil || !token.Valid {
		return ""
	}
	return claim(token, claimName)
}

// bearerToken returns the token in a "Bearer <token>" header, or "".
func bearerToken(authHeader string) string {
	tokenStr := strings.TrimPrefix(authHeader, "Bearer ")
	if tokenStr == authHeader {
		return "" // no "Bearer " prefix found
	}
	return tokenStr
}

// claim returns a string claim from a parsed token, or "".
func claim(token *jwt.Token, name string) string {
	claims, ok := token.Claims.(jwt.MapClaims)
	if !ok {
		return ""
	}
	if val, ok := claims[name].(string); ok {
		return val
	}
	return ""
}
