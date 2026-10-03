// Package apifoundation는 API plane이 각자 소유할 공통 서비스 생성 순서를 제공한다.
package apifoundation

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
	"github.com/kapu/hololive-shared/pkg/service/member"
)

type ScraperHolodexOptions struct {
	Holodex          settings.HolodexConfig
	OfficialSchedule settings.OfficialScheduleRuntimeConfig
}

type ScraperHolodexFoundation struct {
	HolodexService       *holodexprovider.Service
	MemberServiceAdapter domain.MemberDataProvider
}

// BuildScraperHolodex는 적재한 설정과 plane별 cache로 별개의 foundation을 만든다.
func BuildScraperHolodex(
	ctx context.Context,
	options ScraperHolodexOptions,
	memberCache *member.Cache,
	cacheClient cache.Client,
	logger *slog.Logger,
) (*ScraperHolodexFoundation, error) {
	memberServiceAdapter := providers.ProvideMemberServiceAdapter(ctx, memberCache, logger)

	scraperService, err := providers.ProvideScraperServiceWithOfficialSchedule(
		memberServiceAdapter,
		logger,
		options.OfficialSchedule,
	)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	holodexService, err := providers.ProvideHolodexServiceWithConfig(&options.Holodex, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("provide holodex service: %w", err)
	}

	return &ScraperHolodexFoundation{
		HolodexService:       holodexService,
		MemberServiceAdapter: memberServiceAdapter,
	}, nil
}
