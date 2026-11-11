package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

// newAlarmClient는 bot plane의 alarm 데이터 원천인 alarm-worker HTTP client를 만든다. 원천은 이 provider 하나다
// (hololive-api bot·admin plane 공통). ALARM_INTERNAL_URL은 apiplane.LoadRuntime이 필수로 검증하므로 in-process
// AlarmService 분기는 두지 않고, URL이 비면 오류로 끝낸다. 반환한 client는 호출자가 닫는다.
func newAlarmClient(appConfig *apiconfig.BotPlaneConfig, logger *slog.Logger) (*alarmhttp.Client, error) {
	providerURL := strings.TrimSpace(appConfig.AlarmServiceURL)
	if providerURL == "" {
		return nil, errors.New("alarm provider URL (ALARM_INTERNAL_URL) is required")
	}

	alarmClient, err := alarmhttp.NewClientWithAPIKeyStrict(providerURL, appConfig.Server.APIKey, logger, appConfig.InternalH3)
	if err != nil {
		return nil, fmt.Errorf("configure alarm worker client: %w", err)
	}

	return alarmClient, nil
}
