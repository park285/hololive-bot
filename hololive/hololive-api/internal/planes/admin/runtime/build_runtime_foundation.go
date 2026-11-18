package adminruntime

import (
	"errors"
	"fmt"
	"log/slog"
	"strings"

	apiconfig "github.com/kapu/hololive-api/internal/config"
	"github.com/kapu/hololive-shared/pkg/service/alarmhttp"
)

func buildAlarmProvider(
	appConfig *apiconfig.AdminPlaneConfig,
	logger *slog.Logger,
) (alarmProvider, error) {
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
