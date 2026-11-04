// Package middleware provides HTTP middleware for automatic LLM call attribution.
//
// echo.go extracts user_id and project_id from requests and injects them into the
// context so downstream otellix.Trace() calls pick them up automatically.
//
// Budgets only apply limits to verified identity, so read the user from
// your auth middleware, or have the middleware verify the JWT:
//
//	e.Use(authMiddleware) // sets c.Set("user_id", ...) after verifying the token
//	e.Use(otellixmw.EchoMiddleware(otellixmw.WithUserFromContext("user_id")))
//
//	e.Use(otellixmw.EchoMiddleware(otellixmw.WithVerifiedJWT(keyFunc)))
//
// With no options, the user comes from the JWT's "sub" claim unverified, and
// no project is read. See GinMiddleware for the details.
package middleware

import (
	echo "github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/oluwajubelo1/otellix/internal/identity"
)

// EchoMiddleware returns an Echo middleware that extracts user_id and project_id
// from requests and injects them into the context for downstream Trace() calls.
func EchoMiddleware(opts ...MiddlewareOption) echo.MiddlewareFunc {
	cfg := identity.Default()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tracer := otel.Tracer(identity.TracerName)
			ctx, span := tracer.Start(c.Request().Context(), "http.request",
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			userID, projectID := cfg.Resolve(c.Get, c.Request().Header.Get)
			ctx = cfg.Attribute(ctx, span, userID, projectID)

			// Set request attributes.
			span.SetAttributes(
				attribute.String("http.method", c.Request().Method),
				attribute.String("http.route", c.Path()),
			)

			c.SetRequest(c.Request().WithContext(ctx))

			err := next(c)

			// Set response status.
			span.SetAttributes(attribute.Int("http.status_code", c.Response().Status))

			return err
		}
	}
}
