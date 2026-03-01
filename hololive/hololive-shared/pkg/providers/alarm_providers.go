package providers

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

// ProvideAlarmRepository - 알람 저장소 생성 (DB 영속화)
func ProvideAlarmRepository(
	postgres *database.PostgresService,
	logger *slog.Logger,
) *alarm.Repository {
	return alarm.NewRepository(postgres, logger)
}

// ProvideChzzkClient - Chzzk API 클라이언트 생성
func ProvideChzzkClient(httpClient *http.Client, cfg config.ChzzkConfig, logger *slog.Logger) *chzzk.Client {
	return chzzk.NewClientWithConfig(chzzk.ClientConfig{
		HTTPClient:   httpClient,
		BaseURL:      chzzk.DefaultBaseURL,
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
		Logger:       logger,
	})
}

// ProvideTwitchClient - Twitch Helix API 클라이언트 생성
func ProvideTwitchClient(cfg config.TwitchConfig, logger *slog.Logger) *twitch.Client {
	return twitch.NewClient(twitch.ClientConfig{
		ClientID:     cfg.ClientID,
		ClientSecret: cfg.ClientSecret,
	}, logger)
}

// ProvideAlarmService - 알림 서비스 생성
func ProvideAlarmService(
	advanceMinutes []int,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepo *alarm.Repository,
	logger *slog.Logger,
) (*notification.AlarmService, error) {
	svc, err := notification.NewAlarmService(
		cacheSvc,
		holodexSvc,
		chzzkClient,
		twitchClient,
		memberData,
		alarmRepo,
		logger,
		advanceMinutes,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm service: %w", err)
	}
	return svc, nil
}

// ProvideMajorEventRepository - 대형 행사 구독 저장소 생성
func ProvideMajorEventRepository(
	ctx context.Context,
	postgres *database.PostgresService,
	logger *slog.Logger,
	autoPrepareSchema bool,
) *majorevent.Repository {
	repo := majorevent.NewRepository(postgres, logger)
	if autoPrepareSchema {
		if err := repo.CreateTable(ctx); err != nil {
			logger.Error("Failed to create major_event_subscriptions table", slog.String("error", err.Error()))
		}
	}
	return repo
}
