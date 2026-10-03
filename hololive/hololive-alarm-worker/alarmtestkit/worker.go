// Package alarmtestkit은 모듈 경계를 넘는 회귀에서 실제 worker 구독 서비스와 HTTP handler를 구성합니다.
package alarmtestkit

import (
	"fmt"
	"log/slog"
	"net/http"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/subscriptions"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

// NewWorker는 target 변경에 필요한 실제 구독 서비스와 HTTP 경로, 적용된 target 조회를 반환합니다.
func NewWorker(targetMinutes []int) (http.Handler, func() []int, error) {
	logger := slog.New(slog.DiscardHandler)

	service, err := subscriptions.NewAlarmService(cachemocks.NewLenientClient(), nil, &alarm.Repository{}, logger, targetMinutes)
	if err != nil {
		return nil, nil, fmt.Errorf("create alarm test worker: %w", err)
	}

	worker := gin.New()
	alarm.NewHandler(service, logger).RegisterInternalRoutes(worker.Group("/"))

	return worker, service.GetTargetMinutes, nil
}
