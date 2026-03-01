package server

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	sharedserver "github.com/kapu/hololive-shared/pkg/server"
)

func (h *APIHandler) respondError(c *gin.Context, status int, message string, extra gin.H) {
	sharedserver.RespondError(c, status, message, extra)
}

func (h *APIHandler) respondInternalError(c *gin.Context, userMessage, logMessage string, err error, attrs ...slog.Attr) {
	sharedserver.RespondInternalError(h.logger, c, userMessage, logMessage, err, attrs...)
}
