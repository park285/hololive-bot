package app

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
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
