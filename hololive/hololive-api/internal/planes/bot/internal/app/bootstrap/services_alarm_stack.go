package bootstrap

import (
	"fmt"
	"log/slog"

	"github.com/park285/iris-client-go/v3/iris"

	messageformatter "github.com/kapu/hololive-api/internal/planes/bot/internal/adapter/messaging/formatter"
	"github.com/kapu/hololive-api/internal/planes/bot/internal/service/matcher"
	"github.com/kapu/hololive-api/internal/service/activity"
	configsettings "github.com/kapu/hololive-shared/pkg/config/settings"
	sharedmodules "github.com/kapu/hololive-shared/pkg/providers/modules"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

type AlarmYouTubeStackComponents struct {
	AlarmMode       *AlarmModeComponents
	Matcher         *matcher.Matcher
	ActivityLogger  *activity.Logger
	SettingsService settings.ReadWriter
}

func InitAlarmYouTubeStack(
	appConfig *configsettings.Config,
	foundation *ScraperHolodexFoundation,
	_ iris.Sender,
	_ *messageformatter.ResponseFormatter,
	logger *slog.Logger,
) (*AlarmYouTubeStackComponents, error) {
	alarmMode, err := InitAlarmModeComponents(appConfig, foundation.MemberServiceAdapter, logger)
	if err != nil {
		return nil, fmt.Errorf("init alarm mode components: %w", err)
	}

	memberMatcher := ProvideMatcher(alarmMode.MemberDataSource, logger)

	settingsService, err := sharedmodules.BuildSettingsService(
		appConfig.SettingsFilePath,
		appConfig.Notification.AdvanceMinutes,
		logger,
	)
	if err != nil {
		return nil, fmt.Errorf("build settings service: %w", err)
	}

	return &AlarmYouTubeStackComponents{
		AlarmMode:       alarmMode,
		Matcher:         memberMatcher,
		ActivityLogger:  ProvideActivityLogger(logger),
		SettingsService: settingsService,
	}, nil
}
