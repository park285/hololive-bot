package bootstrap

import (
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-api/internal/apifoundation"
	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
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
	alarmMode, err := InitAlarmModeComponents(appConfig, foundation.MemberData, logger)
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

	return &AlarmYouTubeStackComponents{
		AlarmMode: alarmMode,
		Matcher:   memberMatcher,
	}, nil
}
