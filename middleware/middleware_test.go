package middleware

import (
	"net/http"
	"net/http/httptest"
	"testing"

	"github.com/gin-gonic/gin"
	jwt "github.com/golang-jwt/jwt/v5"
	echo "github.com/labstack/echo/v4"

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

type identityCase struct {
	name        string
	opts        []MiddlewareOption
	authUser    string // set in the framework context by a fake auth middleware
	authProject string
	jwtSub      string
	header      string
	wantUser    string
	wantProject string
}

func identityCases() []identityCase {
	return []identityCase{
		{
			name:        "default reads JWT sub and project header",
			jwtSub:      "jwt_user",
			header:      "header_project",
			wantUser:    "jwt_user",
			wantProject: "header_project",
		},
		{
			name:        "no credentials leaves identity empty",
			wantUser:    "",
			wantProject: "",
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
			name:     "context key with no verified user leaves user empty",
			opts:     []MiddlewareOption{WithUserFromContext("user_id")},
			jwtSub:   "forged_user",
			wantUser: "",
		},
	}
}

func newRequest(t *testing.T, tc identityCase) *http.Request {
	req := httptest.NewRequest(http.MethodPost, "/generate", nil)
	if tc.jwtSub != "" {
		req.Header.Set("Authorization", "Bearer "+unverifiedToken(t, tc.jwtSub))
	}
	if tc.header != "" {
		req.Header.Set("X-Project-ID", tc.header)
	}
	return req
}

func TestGinMiddlewareIdentity(t *testing.T) {
	gin.SetMode(gin.TestMode)
	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotUser, gotProject string
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
				gotUser = otellix.UserFromContext(c.Request.Context())
				gotProject = otellix.ProjectFromContext(c.Request.Context())
			})

			r.ServeHTTP(httptest.NewRecorder(), newRequest(t, tc))

			if gotUser != tc.wantUser || gotProject != tc.wantProject {
				t.Errorf("got user=%q project=%q, want user=%q project=%q", gotUser, gotProject, tc.wantUser, tc.wantProject)
			}
		})
	}
}

func TestEchoMiddlewareIdentity(t *testing.T) {
	for _, tc := range identityCases() {
		t.Run(tc.name, func(t *testing.T) {
			var gotUser, gotProject string
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
				gotUser = otellix.UserFromContext(c.Request().Context())
				gotProject = otellix.ProjectFromContext(c.Request().Context())
				return nil
			})

			e.ServeHTTP(httptest.NewRecorder(), newRequest(t, tc))

			if gotUser != tc.wantUser || gotProject != tc.wantProject {
				t.Errorf("got user=%q project=%q, want user=%q project=%q", gotUser, gotProject, tc.wantUser, tc.wantProject)
			}
		})
	}
}
