package alarm

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// NewInternalRouteRegistrar returns a RuntimeRouter-compatible registrar for
// the complete alarm HTTP contract. Keeping the Gin-facing route wiring in the
// shared alarm package prevents runtime packages from duplicating the route set
// during staged provider migrations.
func NewInternalRouteRegistrar(apiKey string, alarmCRUD domain.AlarmCRUD, logger *slog.Logger) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		if router == nil || alarmCRUD == nil {
			return nil
		}

		alarmAPI := NewHandler(alarmCRUD, logger)
		internalAlarm := router.Group("")
		internalAlarm.Use(middleware.APIKeyAuthMiddleware(apiKey))
		alarmAPI.RegisterInternalRoutes(internalAlarm)
		return nil
	}
}
