package httpapi

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

func NewInternalRouteRegistrar(apiKey string, alarmService AlarmService, logger *slog.Logger) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		if router == nil {
			return errors.New("register alarm routes: router is nil")
		}

		if alarmService == nil {
			return errors.New("register alarm routes: alarm service is nil")
		}

		alarmAPI := NewHandler(alarmService, logger)
		internalAlarm := router.Group("")
		internalAlarm.Use(middleware.APIKeyAuthMiddleware(apiKey))
		alarmAPI.RegisterInternalRoutes(internalAlarm)

		return nil
	}
}
