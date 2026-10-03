package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
)

// Alarm 데이터 원천은 alarm-worker HTTP provider 하나다(hololive-api bot·admin plane 공통).
// ALARM_INTERNAL_URL은 apiplane.LoadRuntime이 필수로 검증하므로 in-process AlarmService 분기는 두지 않고,
// URL이 비면 오류로 끝낸다.
func InitAlarmModeComponents(
	appConfig *settings.Config,
	memberServiceAdapter domain.MemberDataProvider,
	logger *slog.Logger,
) (*AlarmModeComponents, error) {
	providerURL := strings.TrimSpace(appConfig.AlarmServiceURL)
	if providerURL == "" {
		return nil, errors.New("alarm provider URL (ALARM_INTERNAL_URL) is required")
	}

	alarmClient, err := alarm.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger, appConfig.InternalH3)
	if err != nil {
		return nil, fmt.Errorf("configure alarm worker client: %w", err)
	}

	return &AlarmModeComponents{
		AlarmCRUD:        alarmClient,
		MemberDataSource: memberServiceAdapter,
		AlarmClient:      alarmClient,
	}, nil
}
