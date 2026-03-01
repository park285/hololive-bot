package providers

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/llm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/kapu/hololive-shared/pkg/service/majorevent"
	"github.com/kapu/hololive-shared/pkg/service/matcher"
	"github.com/kapu/hololive-shared/pkg/service/member"
	"github.com/kapu/hololive-shared/pkg/service/membernews"
)

// ProvideMemberRepository - 멤버 저장소 생성
func ProvideMemberRepository(postgres *database.PostgresService, logger *slog.Logger) *member.Repository {
	return member.NewMemberRepository(postgres, logger)
}

// ProvideMemberCache - 멤버 캐시 생성 (초기 워밍업 포함)
func ProvideMemberCache(
	ctx context.Context,
	repo *member.Repository,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) (*member.Cache, error) {
	memberCache, err := buildMemberCache(ctx, repo, cacheSvc, logger)
	if err != nil {
		return nil, err
	}

	if cacheSvc == nil {
		logger.Warn("Cache service is nil; member database init skipped")
		return memberCache, nil
	}

	// Valkey member database 초기화 (이름 -> 채널ID 맵)
	members, err := repo.GetAllMembers(ctx)
	if err != nil {
		logger.Warn("Failed to load members for member database init", slog.Any("error", err))
		members = []*domain.Member{}
	}

	memberMap := make(map[string]string, len(members))
	for _, m := range members {
		if m != nil && m.ChannelID != "" {
			// name:org 형식으로 캐시 키 생성 (동명이인 지원)
			memberMap[m.Name+":"+m.GetOrg()] = m.ChannelID
		}
	}

	if err := cacheSvc.InitializeMemberDatabase(ctx, memberMap); err != nil {
		return nil, fmt.Errorf("failed to initialize member database: %w", err)
	}

	return memberCache, nil
}

// ProvideMemberCacheWithoutValkey - Valkey 없이 멤버 캐시만 구성
func ProvideMemberCacheWithoutValkey(
	ctx context.Context,
	repo *member.Repository,
	logger *slog.Logger,
) (*member.Cache, error) {
	return buildMemberCache(ctx, repo, nil, logger)
}

// ProvideMemberServiceAdapter - 멤버 데이터 제공자 어댑터 생성
func ProvideMemberServiceAdapter(memberCache *member.Cache, logger *slog.Logger) *member.ServiceAdapter {
	return member.NewMemberServiceAdapter(memberCache, logger)
}

// ProvideMembersData - 도메인 인터페이스로 바인딩
func ProvideMembersData(adapterSvc *member.ServiceAdapter) domain.MemberDataProvider {
	return adapterSvc
}

// ProvideProfileService - 프로필 서비스 생성 (번역 사전 로드 포함)
func ProvideProfileService(
	ctx context.Context,
	cacheSvc *cache.Service,
	members *member.ServiceAdapter,
	logger *slog.Logger,
) (*member.ProfileService, error) {
	svc, err := member.NewProfileService(cacheSvc, members, logger)
	if err != nil {
		return nil, fmt.Errorf("failed to create profile service: %w", err)
	}
	svc.PreloadTranslations(ctx)
	return svc, nil
}

// ProvideMemberMatcher - 멤버 매칭 서비스 생성
func ProvideMemberMatcher(
	ctx context.Context,
	membersData domain.MemberDataProvider,
	cacheSvc *cache.Service,
	holodexSvc *holodex.Service,
	logger *slog.Logger,
) *matcher.MemberMatcher {
	// selector는 nil (Gemini AI 채널 선택 미사용)
	return matcher.NewMemberMatcher(ctx, membersData, cacheSvc, holodexSvc, nil, logger)
}

// ProvideMemberNewsRepository: 구독 멤버 뉴스 저장소 생성
func ProvideMemberNewsRepository(
	postgres *database.PostgresService,
	cacheSvc *cache.Service,
	logger *slog.Logger,
) *membernews.Repository {
	return membernews.NewRepository(postgres, cacheSvc, logger)
}

// ProvideMemberNewsService: 구독 멤버 뉴스 서비스 생성
func ProvideMemberNewsService(
	ctx context.Context,
	repo *membernews.Repository,
	llmClient llm.Client,
	reviewerClient llm.Client,
	adjudicatorClient llm.Client,
	searcher majorevent.WebSearcher,
	membersData domain.MemberDataProvider,
	memberNewsCfg config.ConsensusLLMConfig,
	logger *slog.Logger,
) *membernews.Service {
	allowlistPath := resolveMemberNewsXAllowlistPath()
	validator, err := membernews.NewSourceValidator(allowlistPath, membersData, logger)
	if err != nil {
		logger.Warn("Failed to load member news x allowlist, fallback to empty allowlist",
			slog.String("path", allowlistPath),
			slog.String("error", err.Error()),
		)
		validator, _ = membernews.NewSourceValidator("", membersData, logger)
	}

	adaptedSearcher := &memberNewsSearcherAdapter{base: searcher}
	baseSummarizer := membernews.NewSummarizer(llmClient, adaptedSearcher, validator, logger)

	var summarizer membernews.Summarizer = baseSummarizer
	if memberNewsCfg.Enabled && reviewerClient != nil {
		summarizer = membernews.NewConsensusSummarizer(
			baseSummarizer, reviewerClient, adjudicatorClient, validator,
			membernews.ConsensusConfig{
				ConfidenceThreshold: memberNewsCfg.Confidence,
				ReviewTimeout:       time.Duration(memberNewsCfg.ReviewTimeout) * time.Second,
				AdjudicateTimeout:   time.Duration(memberNewsCfg.AdjudicateTimeout) * time.Second,
			},
			logger,
		)
		logger.Info("Consensus summarizer enabled",
			slog.Float64("confidence_threshold", memberNewsCfg.Confidence),
			slog.Int("review_timeout_sec", memberNewsCfg.ReviewTimeout),
			slog.Int("adjudicate_timeout_sec", memberNewsCfg.AdjudicateTimeout),
		)
	}

	service := membernews.NewService(repo, summarizer, validator, membersData, logger)
	if warmErr := service.WarmupSubscriptionCache(ctx); warmErr != nil {
		logger.Warn("Member news subscription warmup failed", slog.String("error", warmErr.Error()))
	}
	return service
}
