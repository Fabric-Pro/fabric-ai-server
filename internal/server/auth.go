package restapi

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
)

const APIKeyHeader = "X-API-Key"

// APIKeyMiddleware validates API key for protected endpoints.
// Swagger documentation endpoints (/swagger/*) and health check endpoints
// (/health, /ready) are exempt from authentication.
func APIKeyMiddleware(apiKey string) gin.HandlerFunc {
	return func(c *gin.Context) {
		path := c.Request.URL.Path

		// Skip authentication for Swagger documentation endpoints
		// This allows public access to API docs even when authentication is enabled
		if strings.HasPrefix(path, "/swagger/") {
			c.Next()
			return
		}

		// Skip authentication for health check endpoints
		// These must be unauthenticated for container orchestrators (Kubernetes, Aspire, etc.)
		if path == "/health" || path == "/ready" {
			c.Next()
			return
		}

		headerApiKey := c.GetHeader(APIKeyHeader)

		if headerApiKey == "" {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Missing API Key"})
			return
		}

		if headerApiKey != apiKey {
			c.AbortWithStatusJSON(http.StatusUnauthorized, gin.H{"error": "Wrong API Key"})
			return
		}

		c.Next()
	}
}
