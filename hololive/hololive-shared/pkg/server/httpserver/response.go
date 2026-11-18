package httpserver

import (
	"log/slog"
	"maps"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/ginjson"
)

const responseKeyError = "error"

func respondJSON(c *gin.Context, status int, payload any) {
	ginjson.Respond(c, status, payload)
}

func RespondError(c *gin.Context, status int, message string, extra gin.H) {
	payload := gin.H{responseKeyError: message}
	maps.Copy(payload, extra)
	respondJSON(c, status, payload)
}

func RespondInternalError(
	logger *slog.Logger,
	c *gin.Context,
	userMessage,
	logMessage string,
	err error,
	attrs ...slog.Attr,
) {
	if logger != nil {
		logAttrs := make([]any, 0, len(attrs)+1)

		logAttrs = append(logAttrs, slog.Any("error", err))

		for _, attr := range attrs {
			logAttrs = append(logAttrs, attr)
		}

		logger.Error(logMessage, logAttrs...)
	}

	RespondError(c, 500, userMessage, nil)
}
