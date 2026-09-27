package modules

import (
	"fmt"
	"log/slog"

	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

// BuildSettingsService는 hololive-api bot·admin plane이 쓰는 settings 서비스를 만든다. 저장 파일을 읽지 못하거나 계약에 맞지
// 않으면 기동을 멈추도록 오류를 돌려준다.
func BuildSettingsService(settingsPath string, targetMinutes []int, logger *slog.Logger) (settings.ReadWriter, error) {
	if logger != nil {
		logger.Info("Using settings file path", slog.String("path", settingsPath))
	}

	policy := sharedchecker.NewTargetMinutePolicyFromConfigured(targetMinutes)

	service, err := settings.NewSettingsService(settingsPath, settings.Settings{
		AlarmAdvanceMinutes: policy.PrimaryAdvanceMinute(),
		TargetMinutes:       policy.Clone(),
	}, logger)
	if err != nil {
		return nil, fmt.Errorf("build settings service: %w", err)
	}

	return service, nil
}

// ResolvePersistedTargetMinutes는 alarm-worker 기동 때 관리 화면이 저장한 알림 시점을 읽는다. 파일이 없을 때만 설정값을
// 쓴다. 파일 해석은 settings.ReadFile 하나가 소유하므로, targetMinutes 없는 구형 형식이나 읽기·decode 실패는 설정값으로
// 대신하지 않고 오류로 드러난다(stack-audit 2026-09-26 T11 holo-api-persisted-settings-legacy-format,
// holo-settings-file-legacy-format-and-dual-reader).
func ResolvePersistedTargetMinutes(settingsPath string, targetMinutes []int, logger *slog.Logger) ([]int, error) {
	stored, found, err := settings.ReadFile(settingsPath)
	if err != nil {
		return nil, fmt.Errorf("resolve persisted target minutes: %w", err)
	}

	if !found {
		resolvedConfigured := sharedchecker.NewTargetMinutePolicyFromConfigured(targetMinutes).Clone()
		logResolvedTargetMinutes(logger, "config-missing", resolvedConfigured)

		return resolvedConfigured, nil
	}

	logResolvedTargetMinutes(logger, "persisted-settings", stored.TargetMinutes)

	return stored.TargetMinutes, nil
}

func logResolvedTargetMinutes(logger *slog.Logger, source string, resolved []int) {
	if logger == nil {
		return
	}

	logger.Info("Resolved target minutes",
		slog.String("source", source),
		slog.Any("resolved_target_minutes", resolved),
	)
}
