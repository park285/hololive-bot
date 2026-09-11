package workerapp

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/constants"
	contractssettings "github.com/kapu/hololive-shared/pkg/contracts/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func buildAlarmAdvanceMinutesHandler(ctx context.Context, alarmCRUD domain.AlarmCRUD, logger *slog.Logger) func(contractssettings.AlarmAdvanceMinutesPayloadV1) {
	// 런타임 콜백은 build 취소와 분리하고 요청마다 기존 관리 요청 상한을 적용합니다.
	applyBase := context.WithoutCancel(ctx)

	return func(payload contractssettings.AlarmAdvanceMinutesPayloadV1) {
		applyCtx, cancel := context.WithTimeout(applyBase, constants.RequestTimeout.AdminRequest)
		defer cancel()

		targets := alarmCRUD.UpdateAlarmAdvanceMinutes(applyCtx, payload.Minutes)

		if logger != nil {
			logger.Info("Alarm worker applied alarm advance minutes via pub/sub", slog.Int("minutes", payload.Minutes), slog.Any("targets", targets))
		}
	}
}
