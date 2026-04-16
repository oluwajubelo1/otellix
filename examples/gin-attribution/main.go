package main

import (
	"context"
	"fmt"
	"net/http"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/oluwajubelo1/otellix"
	otellixmw "github.com/oluwajubelo1/otellix/middleware"
	"github.com/oluwajubelo1/otellix/providers/openai"
)

func main() {
	// 1. Setup Otellix for local development.
	// This will print clean, human-readable summaries of LLM calls to stdout.
	shutdown := otellix.SetupDev()
	defer shutdown()

	r := gin.Default()

	// 2. Attach Otellix middleware.
	// It will extract X-Project-ID from headers and UserID from JWT Bearer tokens.
	r.Use(otellixmw.GinMiddleware(
		otellixmw.WithProjectHeader("X-Tenant-ID"),
	))

	// 3. Define a route that calls an LLM.
	r.GET("/joke", func(c *gin.Context) {
		provider := openai.New()

		// 🚀 IMPORTANT: Notice we DON'T pass otellix.WithUserID or WithProjectID here.
		// The Trace() function pulls them automatically from the request context
		// thanks to the middleware above.
		_, err := otellix.Trace(c.Request.Context(), provider, providers.CallParams{
			Model:    "gpt-4o",
			Messages: []providers.Message{{Role: "user", Content: "Tell me a joke about SREs."}},
		},
			otellix.WithFeatureID("joke-generator"),
		)

		if err != nil {
			c.JSON(http.StatusInternalServerError, gin.H{"error": err.Error()})
			return
		}

		c.JSON(http.StatusOK, gin.H{"message": "Check your terminal for the attributed Otellix span!"})
	})

	fmt.Println("\n🚀 Otellix Gin Attribution Demo")
	fmt.Println("1. Run: curl -H 'X-Tenant-ID: raven-corp' http://localhost:8080/joke")
	fmt.Println("2. Observe the 'project: raven-corp' tag in the terminal output.")

	r.Run(":8080")
}
