package middleware

import (
	"net/http"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/park285/shared-go/v2/pkg/httputil"

	"github.com/kapu/hololive-shared/pkg/server/ginjson"
)

func errorPayload(code, message string) gin.H {
	payload := gin.H{"error": code}

	if message != "" {
		payload["message"] = message
	}

	return payload
}

func abortWithError(c *gin.Context, status int, code, message string) {
	c.Abort()
	ginjson.Respond(c, status, errorPayload(code, message))
}

func respondWithError(c *gin.Context, status int, code, message string) {
	ginjson.Respond(c, status, errorPayload(code, message))
}

func APIKeyAuthMiddleware(apiKey string) gin.HandlerFunc {
	expected := strings.TrimSpace(apiKey)

	return func(c *gin.Context) {
		if expected == "" {
			abortWithError(c, http.StatusServiceUnavailable, "auth_not_configured", "API key not configured")

			return
		}

		providedKey := c.GetHeader(httputil.HeaderAPIKey)
		if providedKey == "" {
			abortWithError(c, http.StatusUnauthorized, "unauthorized", "API key required")

			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, expected) {
			abortWithError(c, http.StatusForbidden, "forbidden", "invalid API key")

			return
		}

		c.Next()
	}
}

func NoRouteAuthHandler(apiKey string) gin.HandlerFunc {
	expected := strings.TrimSpace(apiKey)

	return func(c *gin.Context) {
		if expected == "" {
			respondWithError(c, http.StatusServiceUnavailable, "auth_not_configured", "API key not configured")

			return
		}

		providedKey := c.GetHeader(httputil.HeaderAPIKey)
		if providedKey == "" {
			respondWithError(c, http.StatusUnauthorized, "unauthorized", "API key required")

			return
		}

		if !httputil.ConstantTimeStringEqual(providedKey, expected) {
			respondWithError(c, http.StatusForbidden, "forbidden", "invalid API key")

			return
		}

		respondWithError(c, http.StatusNotFound, "not_found", "endpoint not found")
	}
}
