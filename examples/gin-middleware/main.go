// examples/gin-middleware/main.go — Gin integration with automatic attribution.
//
// Run with:
//
//	ANTHROPIC_API_KEY=xxx go run examples/gin-middleware/main.go
//	curl -H "Authorization: Bearer <jwt>" -H "X-Project-ID: proj_123" http://localhost:8080/ask?q=hello
package main

import (
	"fmt"
	"log"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/oluwajubelo1/otellix"
	"github.com/oluwajubelo1/otellix/middleware"
	"github.com/oluwajubelo1/otellix/providers"
	"github.com/oluwajubelo1/otellix/providers/anthropic"
)

func main() {
	shutdown := otellix.SetupDev()
	defer shutdown()

	provider := anthropic.New()

	r := gin.Default()
	r.Use(middleware.GinMiddleware())

	r.GET("/ask", func(c *gin.Context) {
		question := c.Query("q")
		if question == "" {
			c.JSON(http.StatusBadRequest, gin.H{"error": "missing ?q= parameter"})
			return
		}

		// user_id and project_id are automatically picked up from the
		// middleware-injected context values.
		userID := middleware.UserIDFromContext(c.Request.Context())
		projectID := middleware.ProjectIDFromContext(c.Request.Context())

		result, err := otellix.Trace(
			c.Request.Context(),
			provider,
			providers.CallParams{
				Model:        "claude-sonnet-4-6",
				MaxTokens:    256,
				SystemPrompt: "You are a helpful assistant. Be concise.",
				Messages: []providers.Message{
					{Role: "user", Content: question},
				},
			},
			otellix.WithFeatureID("ask-endpoint"),
			otellix.WithUserID(userID),
			otellix.WithProjectID(projectID),
		)
		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{
			"tokens_in":  result.InputTokens,
			"tokens_out": result.OutputTokens,
			"model":      result.Model,
		})
	})

	fmt.Println("Starting server on :8080...")
	log.Fatal(r.Run(":8080"))
}
