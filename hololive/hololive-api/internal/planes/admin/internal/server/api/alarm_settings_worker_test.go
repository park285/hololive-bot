package api

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"slices"
	"sync"
	"testing"

	"github.com/gin-gonic/gin"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

// alarmSettingsWorker는 worker가 노출하는 shared 알림 HTTP handler에 target 적용만 하는 서비스를 붙인다.
// 적용 의미는 worker subscriptions 테스트가 검증하고, 여기서는 API의 설정 저장·적용 순서만 본다.
type alarmSettingsWorker struct {
	alarmhttp.AlarmHTTPService

	mu      sync.Mutex
	targets []int
}

func newAlarmSettingsWorker(t *testing.T, targets []int) (http.Handler, func() []int) {
	t.Helper()

	service := &alarmSettingsWorker{targets: slices.Clone(targets)}
	router := gin.New()
	alarmhttp.NewHandler(service, slog.New(slog.DiscardHandler)).RegisterInternalRoutes(router.Group("/"))

	return router, service.targetMinutes
}

func (w *alarmSettingsWorker) UpdateAlarmAdvanceMinutes(ctx context.Context, minutes int) (domain.AdvanceMinutesResult, error) {
	result := domain.AdvanceMinutesResult{RequestedMinutes: minutes, Outcome: domain.ApplyRejected}

	if err := ctx.Err(); err != nil {
		return result, fmt.Errorf("update alarm advance minutes: %w", err)
	}

	policy := targetpolicy.NewTargetMinutePolicyFromRuntimeAdvance(minutes)

	w.mu.Lock()

	w.targets = policy.Clone()
	w.mu.Unlock()

	result.Outcome = domain.ApplyConfirmed
	result.TargetMinutes = policy.Clone()

	return result, nil
}

func (w *alarmSettingsWorker) targetMinutes() []int {
	w.mu.Lock()
	defer w.mu.Unlock()

	return slices.Clone(w.targets)
}
