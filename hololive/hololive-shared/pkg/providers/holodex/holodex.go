package holodex

import (
	"context"
	"errors"
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/internal/service/holodex/provider/htmlscraper"
	"github.com/kapu/hololive-shared/pkg/config/settings"
	"github.com/kapu/hololive-shared/pkg/domain"
	holodexprovider "github.com/kapu/hololive-shared/pkg/service/holodex/provider"
)

func ProvideHolodexService(
	holodexCfg *settings.HolodexConfig,
	cacheClient holodexprovider.Cache,
	scraperService *htmlscraper.Service,
	logger *slog.Logger,
) (*holodexprovider.Service, error) {
	if holodexCfg == nil {
		return nil, errors.New("holodex config is nil")
	}

	service, err := holodexprovider.NewHolodexService(holodexCfg, holodexCfg.BaseURL, holodexCfg.APIKey, cacheClient, scraperService, logger)
	if err != nil {
		return nil, fmt.Errorf("create holodex service: %w", err)
	}

	return service, nil
}

func ProvideScraperService(
	ctx context.Context,
	members domain.MemberDataProvider,
	logger *slog.Logger,
	official settings.OfficialScheduleRuntimeConfig,
) (*htmlscraper.Service, error) {
	service, err := htmlscraper.NewService(ctx, members, nil, logger, official)
	if err != nil {
		return nil, fmt.Errorf("provide scraper service: %w", err)
	}

	return service, nil
}
