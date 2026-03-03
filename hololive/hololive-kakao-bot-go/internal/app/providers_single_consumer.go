package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/matcher"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

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
	cacheSvc cache.Client,
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

// ProvideAlarmRepository - 알람 저장소 생성 (DB 영속화)
func ProvideAlarmRepository(
	postgres database.Client,
	logger *slog.Logger,
) *alarm.Repository {
	return alarm.NewRepository(postgres, logger)
}

// ProvideAlarmWorkerPool - 알림 처리용 워커풀 생성
func ProvideAlarmWorkerPool() (*workerpool.Pool, error) {
	cfg := workerpool.DefaultConfig()
	cfg.Size = 10
	pool, err := workerpool.New(cfg)
	if err != nil {
		return nil, fmt.Errorf("failed to create alarm worker pool: %w", err)
	}
	return pool, nil
}

// ProvideMemberMatcher - 멤버 매칭 서비스 생성
func ProvideMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	logger *slog.Logger,
) *matcher.MemberMatcher {
	// selector는 nil (Gemini AI 채널 선택 미사용)
	return matcher.NewMemberMatcher(ctx, membersData, cacheSvc, holodexSvc, nil, logger)
}
