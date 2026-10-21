package settings

import (
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	settingssvc "github.com/kapu/hololive-shared/pkg/service/settings"
)

// BuildSettingsService는 bot·admin plane이 쓰는 settings 서비스를 만든다. 저장 파일을 읽지 못하거나 계약에 맞지
// 않으면 기동을 실패시킨다.
func BuildSettingsService(settingsPath string, targetMinutes []int, logger *slog.Logger) (settingssvc.ReadWriter, error) {
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	policy := targetpolicy.NewTargetMinutePolicyFromConfigured(targetMinutes)

	service, err := settingssvc.NewSettingsService(settingsPath, settingssvc.Settings{
		AlarmAdvanceMinutes: policy.PrimaryAdvanceMinute(),
		TargetMinutes:       policy.Clone(),
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("build settings service: %w", err)
	}

	return service, nil
}
