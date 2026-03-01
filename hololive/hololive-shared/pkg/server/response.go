package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
)

// RespondError는 API 에러 응답 payload를 일관된 형식으로 반환합니다.
func RespondError(c *gin.Context, status int, message string, extra gin.H) {
	payload := gin.H{"error": message}
	for key, value := range extra {
		payload[key] = value
	}
	c.JSON(status, payload)
}

// RespondInternalError는 내부 에러를 로그에 남기고 500 에러 응답을 반환합니다.
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
