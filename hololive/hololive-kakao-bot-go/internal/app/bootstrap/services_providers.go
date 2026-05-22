package bootstrap

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/stats"

	"github.com/kapu/hololive-kakao-bot-go/internal/bot"
	"github.com/kapu/hololive-shared/pkg/service/acl"
	"github.com/kapu/hololive-shared/pkg/service/activity"
)

func ProvideACLService(
	ctx context.Context,
	kakaoACLEnabled bool,
	kakaoACLMode acl.ACLMode,
	kakaoRooms []string,
	postgres database.Client,
	cacheClient cache.Client,
	logger *slog.Logger,
) (*acl.Service, error) {
	svc, err := acl.NewACLService(
		ctx,
		postgres,
		cacheClient,
		logger,
		kakaoACLEnabled,
		kakaoACLMode,
		kakaoRooms,
	)
	if err != nil {
		return nil, fmt.Errorf("failed to create ACL service: %w", err)
	}

	return svc, nil
}

func ProvideActivityLogger(logger *slog.Logger) *activity.Logger {
	return activity.NewActivityLogger("", logger)
}

func ProvideBotDependencies(modules BotDependencyModules) *bot.Dependencies {
	var youTubeStatsRepo stats.StatsCommandRepository
	if statsRepo := modules.Stream.YTStack.GetStatsRepo(); statsRepo != nil {
		youTubeStatsRepo = statsRepo
	}

	var youTubeService = modules.Stream.YTStack.GetService()

	return &bot.Dependencies{
		BotSelfUser:      modules.Core.BotSelfUser,
		IrisBaseURL:      modules.Core.IrisBaseURL,
		Notification:     modules.Core.Notification,
		Logger:           modules.Core.Logger,
		Client:           modules.Messaging.Client,
		MessageAdapter:   modules.Messaging.MessageAdapter,
		Formatter:        modules.Messaging.Formatter,
		Cache:            modules.Data.Cache,
		Postgres:         modules.Data.Postgres,
		MemberRepo:       modules.Data.MemberRepo,
		MemberCache:      modules.Data.MemberCache,
		Holodex:          modules.Stream.Holodex,
		Chzzk:            modules.Stream.ChzzkClient,
		Twitch:           modules.Stream.TwitchClient,
		Profiles:         modules.Data.Profiles,
		Alarm:            modules.Stream.Alarm,
		Matcher:          modules.Stream.MemberMatch,
		MembersData:      modules.Data.MembersData,
		Service:          youTubeService,
		YouTubeStatsRepo: youTubeStatsRepo,
		Activity:         modules.Support.ActivityLogger,
		Settings:         modules.Support.Settings,
		ACL:              modules.Support.ACL,
		MajorEventRepo:   modules.Feature.MajorEventRepo,
		MemberNews:       modules.Feature.MemberNews,
		CommandBuilders:  modules.Feature.CommandBuilders,
		WorkerPool:       modules.Support.WorkerPool,
	}
}
