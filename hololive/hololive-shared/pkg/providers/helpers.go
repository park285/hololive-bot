package providers

import (
	"context"
	"fmt"
	"log/slog"
	"os"
	"path/filepath"
	"strings"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/settings"
)

func buildMemberCache(
	ctx context.Context,
	repo *member.Repository,
	cacheSvc cache.Client,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := member.NewMemberCache(ctx, repo, cacheSvc, logger, member.CacheConfig{
		WarmUp:    true,
		ValkeyTTL: constants.MemberCacheDefaults.ValkeyTTL,
	})
	if err != nil {
		return nil, fmt.Errorf("failed to create member cache: %w", err)
	}
	return memberCache, nil
}

func defaultAlarmAdvanceMinute(advanceMinutes []int) int {
	maxMinute := 0
	for _, minute := range advanceMinutes {
		if minute > maxMinute {
			maxMinute = minute
		}
	}

	if maxMinute <= 0 {
		return 5
	}

	return maxMinute
}

// ResolveAlarmAdvanceMinutes - 설정 파일의 알람 사전 알림 분을 로드하여 반환한다.
// 설정 파일이 없거나 값이 0 이하이면 인자로 받은 advanceMinutes를 그대로 반환한다.
func ResolveAlarmAdvanceMinutes(advanceMinutes []int, scraperProxyEnabled bool, logger *slog.Logger) []int {
	settingsPath := resolveSettingsFilePath()

	if _, err := os.Stat(settingsPath); err != nil {
		if os.IsNotExist(err) {
			return advanceMinutes
		}
		if logger != nil {
			logger.Warn("Failed to stat settings file, using config alarm advance minutes",
				slog.String("path", settingsPath),
				slog.Any("error", err),
			)
		}
		return advanceMinutes
	}

	defaults := settings.Settings{
		AlarmAdvanceMinutes: defaultAlarmAdvanceMinute(advanceMinutes),
		ScraperProxyEnabled: scraperProxyEnabled,
	}

	svc := settings.NewSettingsService(settingsPath, defaults, logger)
	alarmAdvanceMinutes := svc.Get().AlarmAdvanceMinutes
	if alarmAdvanceMinutes <= 0 {
		return advanceMinutes
	}

	if logger != nil {
		logger.Info("Applying persisted alarm advance minutes",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.String("settings_path", settingsPath),
		)
	}

	return []int{alarmAdvanceMinutes, 3, 1}
}

func resolveSettingsFilePath() string {
	// SETTINGS_DIR 환경변수로 오버라이드 가능 (기본: data)
	dir := strings.TrimSpace(os.Getenv("SETTINGS_DIR"))
	if dir == "" {
		dir = "data"
	}
	return filepath.Join(dir, "settings.json")
}
