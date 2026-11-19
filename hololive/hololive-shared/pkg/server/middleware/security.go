package middleware

import (
	"net/http"
	"uuid"

	"github.com/gin-gonic/gin"
	sharedlog "github.com/park285/shared-go/v2/pkg/logging"
)

func SecurityHeadersMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		c.Header("X-Content-Type-Options", "nosniff")
		c.Header("X-Frame-Options", "DENY")
		c.Header("Referrer-Policy", "strict-origin-when-cross-origin")
		c.Header("Content-Security-Policy", "frame-ancestors 'none'")
		c.Header("Strict-Transport-Security", "max-age=31536000; includeSubDomains")
		c.Next()
	}
}

const (
	requestIDHeaderKey = "X-Request-ID"
	maxRequestIDLength = 128
)

func RequestIDMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		reqID := sanitizedRequestID(c.GetHeader(requestIDHeaderKey))
		if reqID == "" {
			reqID = uuid.New().String()
		}

		c.Set("request_id", reqID)

		c.Request = c.Request.WithContext(sharedlog.WithRequestID(c.Request.Context(), reqID))
		c.Header(requestIDHeaderKey, reqID)
		c.Next()
	}
}

const requestIDAllowedBytes = "abcdefghijklmnopqrstuvwxyz" +
	"ABCDEFGHIJKLMNOPQRSTUVWXYZ" +
	"0123456789-_.:"

var requestIDByteAllowed = buildRequestIDByteSet()

func buildRequestIDByteSet() [256]bool {
	var allowed [256]bool

	for i := range len(requestIDAllowedBytes) {
		allowed[requestIDAllowedBytes[i]] = true
	}

	return allowed
}

func sanitizedRequestID(raw string) string {
	if raw == "" || len(raw) > maxRequestIDLength {
		return ""
	}

	for i := range len(raw) {
		if !requestIDByteAllowed[raw[i]] {
			return ""
		}
	}

	return raw
}

const maxBodyBytes int64 = 1 << 20

func MaxBodySizeMiddleware() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Body != nil {
			c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBodyBytes)
		}

		c.Next()
	}
}
