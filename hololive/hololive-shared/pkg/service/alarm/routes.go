package alarm

import (
	"log/slog"

	"github.com/gin-gonic/gin"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// alarm HTTP 계약 전체에 대한 RuntimeRouter 호환 registrar를 반환한다. Gin route 배선을
// 공유 alarm 패키지에 중앙화해, staged provider migration 중 runtime 패키지들이 route set을
// 중복 정의하지 않게 한다.
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
