package httpapi

import (
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// NewInternalRouteRegistrar는 alarm HTTP 계약 전체에 대한 RuntimeRouter registrar를 반환한다.
// 전체 route 집합과 API key 인증 배선을 이 한 곳에 두어 worker runtime 조립이 route를 중복 정의하지 않게 한다.
func NewInternalRouteRegistrar(apiKey string, alarmService AlarmService, logger *slog.Logger) func(*gin.Engine) error {
	return func(router *gin.Engine) error {
		if router == nil || alarmService == nil {
			return nil
		}

		alarmAPI := NewHandler(alarmService, logger)
		internalAlarm := router.Group("")
		internalAlarm.Use(middleware.APIKeyAuthMiddleware(apiKey))
		alarmAPI.RegisterInternalRoutes(internalAlarm)

		return nil
	}
}
