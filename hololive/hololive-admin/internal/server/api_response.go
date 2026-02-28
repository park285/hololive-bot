package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

func (h *APIHandler) respondError(c *gin.Context, status int, message string, extra gin.H) {
	payload := gin.H{"error": message}
	for key, value := range extra {
		payload[key] = value
	}
	c.JSON(status, payload)
}

func (h *APIHandler) respondInternalError(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
	logAttrs := make([]any, 0, len(attrs)+1)
	logAttrs = append(logAttrs, slog.Any("error", err))
	for _, attr := range attrs {
		logAttrs = append(logAttrs, attr)
	}
	h.logger.Error(logMessage, logAttrs...)
	h.respondError(c, 500, userMessage, nil)
}
