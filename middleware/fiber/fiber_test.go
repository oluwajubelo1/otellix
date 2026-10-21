package fiber

import (
	"context"
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gofiber/fiber/v3"
	jwt "github.com/golang-jwt/jwt/v5"
	sdktrace "go.opentelemetry.io/otel/sdk/trace"
	"go.opentelemetry.io/otel/sdk/trace/tracetest"

	"go.opentelemetry.io/otel"

	"github.com/oluwajubelo1/otellix"
)

// unverifiedToken returns a JWT signed with a key the server never checks,
// as a caller forging their identity would send.
func unverifiedToken(t *testing.T, sub string) string {
	t.Helper()
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, jwt.MapClaims{"sub": sub}).SignedString([]byte("attacker-key"))
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

func TestMiddlewareIdentity(t *testing.T) {
	tests := []struct {
		name        string
		opts        []Option
		authUser    string // set in c.Locals by a fake auth middleware
		authProject string
		jwtSub      string
		header      string
		wantUser    string
		wantProject string
	}{
		{
			name:        "default reads JWT sub and project header",
			jwtSub:      "jwt_user",
			header:      "header_project",
			wantUser:    "jwt_user",
			wantProject: "header_project",
		},
		{
			name: "no credentials leaves identity empty",
		},
		{
			name:        "context keys ignore caller-supplied JWT and header",
			opts:        []Option{WithUserFromContext("user_id"), WithProjectFromContext("project_id")},
			authUser:    "verified_user",
			authProject: "verified_project",
			jwtSub:      "forged_user",
			header:      "forged_project",
			wantUser:    "verified_user",
			wantProject: "verified_project",
		},
		{
			name:   "context key with no verified user leaves user empty",
			opts:   []Option{WithUserFromContext("user_id")},
			jwtSub: "forged_user",
		},
		{
			name:        "custom claim and header",
			opts:        []Option{WithJWTClaim("sub"), WithProjectHeader("X-Tenant-ID")},
			jwtSub:      "jwt_user",
			wantUser:    "jwt_user",
			wantProject: "", // X-Project-ID is no longer read
			header:      "header_project",
		},
	}

	for _, tc := range tests {
		t.Run(tc.name, func(t *testing.T) {
			var gotUser, gotProject string
			app := fiber.New()
			app.Use(func(c fiber.Ctx) error {
				if tc.authUser != "" {
					c.Locals("user_id", tc.authUser)
				}
				if tc.authProject != "" {
					c.Locals("project_id", tc.authProject)
				}
				return c.Next()
			})
			app.Use(Middleware(tc.opts...))
			app.Post("/generate", func(c fiber.Ctx) error {
				gotUser = otellix.UserFromContext(c.Context())
				gotProject = otellix.ProjectFromContext(c.Context())
				return nil
			})

			req := httptest.NewRequest(http.MethodPost, "/generate", nil)
			if tc.jwtSub != "" {
				req.Header.Set("Authorization", "Bearer "+unverifiedToken(t, tc.jwtSub))
			}
			if tc.header != "" {
				req.Header.Set("X-Project-ID", tc.header)
			}
			if _, err := app.Test(req); err != nil {
				t.Fatalf("request: %v", err)
			}

			if gotUser != tc.wantUser || gotProject != tc.wantProject {
				t.Errorf("got user=%q project=%q, want user=%q project=%q", gotUser, gotProject, tc.wantUser, tc.wantProject)
			}
		})
	}
}

func TestMiddlewareSpan(t *testing.T) {
	recorder := tracetest.NewSpanRecorder()
	tp := sdktrace.NewTracerProvider(sdktrace.WithSpanProcessor(recorder))
	prev := otel.GetTracerProvider()
	otel.SetTracerProvider(tp)
	t.Cleanup(func() {
		otel.SetTracerProvider(prev)
		_ = tp.Shutdown(context.Background())
	})

	app := fiber.New()
	app.Use(Middleware())
	app.Get("/missing/:id", func(c fiber.Ctx) error {
		return fiber.ErrNotFound
	})

	req := httptest.NewRequest(http.MethodGet, "/missing/42", nil)
	req.Header.Set("Authorization", "Bearer "+unverifiedToken(t, "span_user"))
	if _, err := app.Test(req); err != nil {
		t.Fatalf("request: %v", err)
	}

	spans := recorder.Ended()
	if len(spans) != 1 {
		t.Fatalf("expected 1 span, got %d", len(spans))
	}
	got := map[string]any{}
	for _, kv := range spans[0].Attributes() {
		got[string(kv.Key)] = kv.Value.AsInterface()
	}

	want := map[string]any{
		"llm.user_id":         "span_user",
		"llm.user_attributed": true,
		"http.method":         "GET",
		"http.route":          "/missing/:id",
		"http.status_code":    int64(404),
	}
	for k, v := range want {
		if got[k] != v {
			t.Errorf("attribute %s = %v, want %v", k, got[k], v)
		}
	}
}
