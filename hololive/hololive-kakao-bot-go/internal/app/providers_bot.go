package app

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/iris"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/settings"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/acl"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/activity"
)

// ProvideACLService - 접근 제어 서비스 생성 (PostgreSQL 영구화)
func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoRooms []string,
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) (*acl.Service, error) {
	svc, err := acl.NewACLService(
		ctx,
		postgres,
		cacheSvc,
		logger,
		kakaoACLEnabled,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACL service: %w", err)
	}
	return svc, nil
}

// ProvideActivityLogger - 활동 로거 생성
func ProvideActivityLogger(logDir string, logger *slog.Logger) *activity.Logger {
	if logDir == "" {
		return activity.NewActivityLogger("", logger)
	}
	return activity.NewActivityLogger(logDir+"/activity.log", logger)
}

// ProvideBotDependencies - 모든 의존성을 bot.Dependencies로 조립
func ProvideBotDependencies(
	botSelfUser string,
	irisBaseURL string,
	notifCfg config.NotificationConfig,
	logger *slog.Logger,
	irisClient iris.Client,
	msgStack *providers.MessageStack,
	cacheSvc *cache.Service,
	postgres *database.PostgresService,
	memberRepo *member.Repository,
	memberCache *member.Cache,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	profiles *member.ProfileService,
	alarmSvc domain.AlarmCRUD,
	memberMatcher *matcher.MemberMatcher,
	membersData domain.MemberDataProvider,
	ytStack *providers.YouTubeStack,
	activityLogger *activity.Logger,
	settingsSvc *settings.Service,
	aclSvc *acl.Service,
	majorEventRepo *majorevent.Repository,
	memberNewsSvc *membernews.Service,
	workerPool *workerpool.Pool,
) *bot.Dependencies {
	return &bot.Dependencies{
		BotSelfUser:      botSelfUser,
		IrisBaseURL:      irisBaseURL,
		Notification:     notifCfg,
		Logger:           logger,
		Client:           irisClient,
		MessageAdapter:   msgStack.Adapter,
		Formatter:        msgStack.Formatter,
		Cache:            cacheSvc,
		Postgres:         postgres,
		MemberRepo:       memberRepo,
		MemberCache:      memberCache,
		Holodex:          holodexSvc,
		Chzzk:            chzzkClient,
		Twitch:           twitchClient,
		Profiles:         profiles,
		Alarm:            alarmSvc,
		Matcher:          memberMatcher,
		MembersData:      membersData,
		Service:          ytStack.Service,
		Scheduler:        ytStack.Scheduler,
		YouTubeStatsRepo: ytStack.StatsRepo,
		Activity:         activityLogger,
		Settings:         settingsSvc,
		ACL:              aclSvc,
		MajorEventRepo:   majorEventRepo,
		MemberNews:       memberNewsSvc,
		WorkerPool:       workerPool,
	}
}
