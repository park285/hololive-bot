package httpapi

import (
	"errors"
	"log/slog"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/server/middleware"
)

// NewInternalRouteRegistrar는 alarm HTTP 계약 전체에 대한 RuntimeRouter registrar를 반환한다.
// 전체 route 집합과 API key 인증 배선을 이 한 곳에 두어 worker runtime 조립이 route를 중복 정의하지 않게 한다.
// 전달받은 router나 alarm service가 nil이면 alarm route 없이 기동하지 않도록 오류를 돌려주며, RuntimeRouter 생성이 이 오류로 실패한다.
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
