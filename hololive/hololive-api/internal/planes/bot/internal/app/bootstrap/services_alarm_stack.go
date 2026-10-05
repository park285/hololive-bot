package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
)

type AlarmYouTubeStackComponents struct {
	AlarmMode *AlarmModeComponents
	Matcher   *matcher.Matcher
}

func InitAlarmYouTubeStack(
	appConfig *apiconfig.BotPlaneConfig,
	foundation *apifoundation.ScraperHolodexFoundation,
	logger *slog.Logger,
) (_ *AlarmYouTubeStackComponents, retErr error) {
	alarmMode, err := InitAlarmModeComponents(appConfig, foundation.MemberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("init alarm mode components: %w", err)
	}

	defer func() {
		if retErr != nil && alarmMode.AlarmClient != nil {
			if closeErr := alarmMode.AlarmClient.Close(); closeErr != nil {
				retErr = errors.Join(retErr, fmt.Errorf("rollback alarm client: %w", closeErr))
			}
		}
	}()

	memberMatcher := ProvideMatcher(alarmMode.MemberDataSource, logger)

	// orchestration에 전달하지 않아도 settings 파일 기동 검증은 유지한다.
	_, err = sharedmodules.BuildSettingsService(
		appConfig.SettingsFilePath,
		appConfig.Notification.AdvanceMinutes,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build settings service: %w", err)
	}

	return &AlarmYouTubeStackComponents{
		AlarmMode: alarmMode,
		Matcher:   memberMatcher,
	}, nil
}
