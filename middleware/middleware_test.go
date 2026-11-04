package middleware

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"testing"
	"time"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	echo "github.com/labstack/echo/v4"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/providers"
)

// serverKey is the key the server verifies tokens with. Tokens signed with
// any other key are forged.
var serverKey = []byte("server-key")

func verifyWithServerKey() MiddlewareOption {
	return WithVerifiedJWT(
		func(*jwt.Token) (any, error) { return serverKey, nil },
		jwt.WithValidMethods([]string{"HS256"}),
	)
}

// token returns a JWT for sub signed with key, already expired if expired is set.
func token(t *testing.T, sub string, key []byte, expired bool) string {
	t.Helper()
	claims := jwt.MapClaims{"sub": sub}
	if expired {
		claims["exp"] = time.Now().Add(-time.Hour).Unix()
	}
	tok, err := jwt.NewWithClaims(jwt.SigningMethodHS256, claims).SignedString(key)
	if err != nil {
		t.Fatalf("sign token: %v", err)
	}
	return tok
}

type identityCase struct {
	name        string
	opts        []MiddlewareOption
	authUser    string // set in the framework context by a fake auth middleware
	authProject string
	jwtSub      string
	jwtKey      []byte // signs the JWT; default: a key the server doesn't know
	jwtExpired  bool
	header      string
	wantUser    string
	wantProject string
	// wantRefused is why a budget with per-user and per-project limits
	// should refuse the call: the Kind of UnverifiedIdentityError, or
	// "unattributed" for an UnattributedCallError, or "" if it's allowed.
	wantRefused string
}

func identityCases() []identityCase {
	return []identityCase{
		{
			name:        "default reads the JWT sub unverified, and no project header",
			jwtSub:      "jwt_user",
			header:      "header_project",
			wantUser:    "jwt_user",
			wantProject: "",
			wantRefused: "user",
		},
		{
			name:        "no credentials leaves identity empty",
			wantUser:    "",
			wantProject: "",
			wantRefused: "unattributed",
		},
		{
			name:        "context keys ignore caller-supplied JWT and header",
			opts:        []MiddlewareOption{WithUserFromContext("user_id"), WithProjectFromContext("project_id")},
			authUser:    "verified_user",
			authProject: "verified_project",
			jwtSub:      "forged_user",
			header:      "forged_project",
			wantUser:    "verified_user",
			wantProject: "verified_project",
		},
		{
			name:        "context key with no verified user leaves user empty",
			opts:        []MiddlewareOption{WithUserFromContext("user_id")},
			jwtSub:      "forged_user",
			wantUser:    "",
			wantRefused: "unattributed",
		},
		{
			name:        "project header is read when opted in, unverified",
			opts:        []MiddlewareOption{WithUserFromContext("user_id"), WithProjectHeader("X-Project-ID")},
			authUser:    "verified_user",
			header:      "header_project",
			wantUser:    "verified_user",
			wantProject: "header_project",
			wantRefused: "project",
		},
		{
			name:     "verified JWT accepts a token signed with the server key",
			opts:     []MiddlewareOption{verifyWithServerKey()},
			jwtSub:   "signed_user",
			jwtKey:   serverKey,
			wantUser: "signed_user",
		},
		{
			name:        "verified JWT rejects a forged signature",
			opts:        []MiddlewareOption{verifyWithServerKey()},
			jwtSub:      "forged_user",
			wantUser:    "",
			wantRefused: "unattributed",
		},
		{
			name:        "verified JWT rejects an expired token",
			opts:        []MiddlewareOption{verifyWithServerKey()},
			jwtSub:      "expired_user",
			jwtKey:      serverKey,
			jwtExpired:  true,
			wantUser:    "",
			wantRefused: "unattributed",
		},
	}
}

func newRequest(t *testing.T, tc identityCase) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/generate", nil)
	if tc.jwtSub != "" {
		key := tc.jwtKey
		if key == nil {
			key = []byte("attacker-key")
		}
		req.Header.Set("Authorization", "Bearer "+token(t, tc.jwtSub, key, tc.jwtExpired))
	}
	if tc.header != "" {
		req.Header.Set("X-Project-ID", tc.header)
	}
	return req
}

// handlerResult is what a request handler saw.
type handlerResult struct {
	user, project string
	refused       string // why a budgeted call was refused, as in identityCase.wantRefused
}

// handle records the identity in ctx, and whether a budgeted call made with
// it is refused for its identity.
func handle(ctx context.Context) handlerResult {
	res := handlerResult{user: otellix.UserFromContext(ctx), project: otellix.ProjectFromContext(ctx)}
	mock := &providers.MockProvider{ProviderName: "openai"}
	budget := &otellix.BudgetConfig{PerUserLimit: 1, PerProjectLimit: 1}
	_, err := otellix.Trace(ctx, mock, providers.CallParams{Model: "gpt-4o"}, otellix.WithBudgetConfig(budget))
	var unverified *otellix.UnverifiedIdentityError
	var unattributed *otellix.UnattributedCallError
	switch {
	case errors.As(err, &unverified):
		res.refused = unverified.Kind
	case errors.As(err, &unattributed):
		res.refused = "unattributed"
	}
	return res
}

func checkResult(t *testing.T, tc identityCase, got handlerResult) {
	t.Helper()
	if got.user != tc.wantUser || got.project != tc.wantProject {
		t.Errorf("got user=%q project=%q, want user=%q project=%q", got.user, got.project, tc.wantUser, tc.wantProject)
	}
	if got.refused != tc.wantRefused {
		t.Errorf("budgeted call refused for %q, want %q", got.refused, tc.wantRefused)
	}
}

func TestGinMiddlewareIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			var got handlerResult
			r := gin.New()
			r.Use(func(c *gin.Context) {
				if tc.authUser != "" {
					c.Set("user_id", tc.authUser)
				}
				if tc.authProject != "" {
					c.Set("project_id", tc.authProject)
				}
			})
			r.Use(GinMiddleware(tc.opts...))
			r.POST("/generate", func(c *gin.Context) {
				got = handle(c.Request.Context())
			})

			r.ServeHTTP(httptest.NewRecorder(), newRequest(t, tc))
			checkResult(t, tc, got)
		})
	}
}

func TestEchoMiddlewareIdentity(t *testing.T) {
	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			var got handlerResult
			e := echo.New()
			e.Use(func(next echo.HandlerFunc) echo.HandlerFunc {
				return func(c echo.Context) error {
					if tc.authUser != "" {
						c.Set("user_id", tc.authUser)
					}
					if tc.authProject != "" {
						c.Set("project_id", tc.authProject)
					}
					return next(c)
				}
			})
			e.Use(EchoMiddleware(tc.opts...))
			e.POST("/generate", func(c echo.Context) error {
				got = handle(c.Request().Context())
				return nil
			})

			e.ServeHTTP(httptest.NewRecorder(), newRequest(t, tc))
			checkResult(t, tc, got)
		})
	}
}
