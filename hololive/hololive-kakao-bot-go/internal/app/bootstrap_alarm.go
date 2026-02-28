package app

import (
	"context"
	"fmt"
	"log/slog"
	"net/http"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/chzzk"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
	"github.com/kapu/hololive-shared/pkg/service/notification"
	"github.com/kapu/hololive-shared/pkg/service/twitch"
)

func initMemberNewsService(
	ctx context.Context,
	cliproxy config.CliproxyConfig,
	llmCfg config.LLMConfig,
	exaCfg config.ExaConfig,
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	membersData domain.MemberDataProvider,
	logger *slog.Logger,
) *membernews.Service {
	repo := providers.ProvideMemberNewsRepository(postgres, cacheSvc, logger)
	llmClient := providers.ProvideMemberNewsLLMClient(cliproxy, llmCfg, logger)
	reviewer := providers.ProvideMemberNewsReviewerClient(cliproxy, llmCfg, logger)
	adjudicator := providers.ProvideMemberNewsAdjudicatorClient(cliproxy, llmCfg, logger)
	searcher := providers.ProvideExaSearcher(exaCfg, logger)
	return providers.ProvideMemberNewsService(ctx, repo, llmClient, reviewer, adjudicator, searcher, membersData, llmCfg.MemberNews, logger)
}

type alarmDependencies struct {
	alarmService       *notification.AlarmService
	memberDataProvider domain.MemberDataProvider
	chzzkClient        *chzzk.Client
	twitchClient       *twitch.Client
}

func initAlarmDependencies(
	chzzkCfg config.ChzzkConfig,
	twitchCfg config.TwitchConfig,
	advanceMinutes []int,
	scraperProxyEnabled bool,
	cacheService *cache.Service,
	holodexService *holodex.Service,
	memberServiceAdapter *member.ServiceAdapter,
	alarmRepository *alarm.Repository,
	logger *slog.Logger,
) (*alarmDependencies, error) {
	httpClient := &http.Client{Timeout: 10 * time.Second}
	chzzkClient := providers.ProvideChzzkClient(httpClient, chzzkCfg, logger)
	twitchClient := providers.ProvideTwitchClient(twitchCfg, logger)
	memberDataProvider := providers.ProvideMembersData(memberServiceAdapter)

	resolved := providers.ResolveAlarmAdvanceMinutes(advanceMinutes, scraperProxyEnabled, logger)
	alarmService, err := providers.ProvideAlarmService(resolved, cacheService, holodexService, chzzkClient, twitchClient, memberDataProvider, alarmRepository, logger)
	if err != nil {
		return nil, fmt.Errorf("provide alarm service: %w", err)
	}

	return &alarmDependencies{
		alarmService:       alarmService,
		memberDataProvider: memberDataProvider,
		chzzkClient:        chzzkClient,
		twitchClient:       twitchClient,
	}, nil
}
