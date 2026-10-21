// Package fiber provides Fiber v3 middleware for automatic LLM call attribution.
//
// It extracts user_id and project_id from requests and stores them in the
// request context, so downstream otellix.Trace() and otellix.TraceStream()
// calls pick them up automatically. Pass c.Context() to those calls:
//
//	app := fiber.New()
//	app.Use(otellixfiber.Middleware())
//
//	app.Post("/generate", func(c fiber.Ctx) error {
//	    res, err := otellix.Trace(c.Context(), provider, params)
//	    ...
//	})
//
// With custom config:
//
//	app.Use(otellixfiber.Middleware(
//	    otellixfiber.WithJWTClaim("user_id"),
//	    otellixfiber.WithProjectHeader("X-Tenant-ID"),
//	))
//
// Reading identity your auth middleware has already verified (recommended
// whenever budgets are enforced):
//
//	app.Use(authMiddleware) // sets c.Locals("user_id", ...) after verifying the token
//	app.Use(otellixfiber.Middleware(otellixfiber.WithUserFromContext("user_id")))
package fiber

import (
	"errors"

	"github.com/gofiber/fiber/v3"
	"go.opentelemetry.io/otel"
	"go.opentelemetry.io/otel/attribute"
	"go.opentelemetry.io/otel/trace"

	"github.com/oluwajubelo1/otellix/internal/identity"
)

// Config holds configuration for the middleware.
type Config = identity.Config

// Option is a functional option for middleware configuration.
type Option func(*Config)

// WithJWTClaim sets the JWT claim name for user_id extraction. Default: "sub".
func WithJWTClaim(claim string) Option {
	return func(c *Config) { c.JWTClaim = claim }
}

// WithProjectHeader sets the HTTP header name for project_id extraction. Default: "X-Project-ID".
func WithProjectHeader(header string) Option {
	return func(c *Config) { c.ProjectHeader = header }
}

// WithUserFromContext reads user_id from c.Locals(key) instead of parsing the
// JWT. Use it with an auth middleware that verifies the caller and stores
// their ID, and register this middleware after it. The value must be a string.
//
// The default JWT extraction does not verify signatures, so on routes without
// verified auth a caller can choose any user_id, including a new one per
// request to sidestep per-user budgets.
func WithUserFromContext(key string) Option {
	return func(c *Config) { c.UserContextKey = key }
}

// WithProjectFromContext reads project_id from c.Locals(key) instead of the
// project header, so callers cannot choose which project's budget they spend.
// The value must be a string.
func WithProjectFromContext(key string) Option {
	return func(c *Config) { c.ProjectContextKey = key }
}

// Middleware returns a Fiber middleware that extracts user_id and project_id
// from requests and injects them into c.Context() for downstream Trace() calls.
func Middleware(opts ...Option) fiber.Handler {
	cfg := identity.Default()
	for _, opt := range opts {
		opt(cfg)
	}

	return func(c fiber.Ctx) error {
		tracer := otel.Tracer(identity.TracerName)
		ctx, span := tracer.Start(c.Context(), "http.request",
			trace.WithSpanKind(trace.SpanKindServer),
		)
		defer span.End()

		userID, projectID := cfg.Resolve(func(key string) any {
			return c.Locals(key)
		}, func(name string) string {
			return c.Get(name)
		})
		ctx = identity.Apply(ctx, span, userID, projectID)

		span.SetAttributes(attribute.String("http.method", c.Method()))

		c.SetContext(ctx)
		err := c.Next()

		// The matched route is only known once the chain has run.
		span.SetAttributes(attribute.String("http.route", c.FullPath()))

		// Fiber's error handler writes the status after the chain returns,
		// so take it from the error when there is one.
		status := c.Response().StatusCode()
		if err != nil {
			status = fiber.StatusInternalServerError
			var fiberErr *fiber.Error
			if errors.As(err, &fiberErr) {
				status = fiberErr.Code
			}
		}
		span.SetAttributes(attribute.Int("http.status_code", status))

		return err
	}
}
