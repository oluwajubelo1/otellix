// Package middleware provides HTTP middleware for automatic LLM call attribution.
//
// echo.go extracts user_id and project_id from requests and injects them into the
// context so downstream otellix.Trace() calls pick them up automatically.
//
// Usage:
//
//	e := echo.New()
//	e.Use(otellixmw.EchoMiddleware())
//
// With custom config:
//
//	e.Use(otellixmw.EchoMiddleware(
//	    otellixmw.WithJWTClaim("user_id"),
//	    otellixmw.WithProjectHeader("X-Tenant-ID"),
//	))
package middleware

import (
	"context"

	"github.com/labstack/echo/v4"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"
)

// EchoMiddleware returns an Echo middleware that extracts user_id and project_id
// from requests and injects them into the context for downstream Trace() calls.
func EchoMiddleware(opts ...MiddlewareOption) echo.MiddlewareFunc {
	cfg := defaultMiddlewareConfig()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(next echo.HandlerFunc) echo.HandlerFunc {
		return func(c echo.Context) error {
			tracer := otel.Tracer("github.com/oluwajubelo1/otellix/middleware")
			ctx, span := tracer.Start(c.Request().Context(), "http.request",
				trace.WithSpanKind(trace.SpanKindServer),
			)
			defer span.End()

			// Extract user_id from JWT.
			userID := extractUserIDFromJWT(c.Request().Header.Get("Authorization"), cfg.JWTClaim)
			if userID != "" {
				span.SetAttributes(attribute.String("llm.user_id", userID))
				ctx = context.WithValue(ctx, userIDKey, userID)
			}

			// Extract project_id from header.
			projectID := c.Request().Header.Get(cfg.ProjectHeader)
			if projectID != "" {
				span.SetAttributes(attribute.String("llm.project_id", projectID))
				ctx = context.WithValue(ctx, projectIDKey, projectID)
			}

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
