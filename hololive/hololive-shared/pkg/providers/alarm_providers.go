package providers

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/database"
)

// ProvideAlarmRepository - 알람 저장소 생성 (DB 영속화)
func ProvideAlarmRepository(
	postgres database.Client,
	logger *slog.Logger,
) *alarm.Repository {
	return alarm.NewRepository(postgres, logger)
}
