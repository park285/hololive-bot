package youtube

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	statscollector "github.com/kapu/hololive-shared/pkg/service/youtube/internal/statscollector"
	ytstats "github.com/kapu/hololive-shared/pkg/service/youtube/stats"
)

type StatsService = statscollector.StatsService

type ChannelStatistics = statscollector.ChannelStatistics

func NewStatsService(oauth *OAuthService, cacheClient cache.Client, statsRepo ytstats.StatsServiceRepository, logger *slog.Logger) *StatsService {
	return statscollector.NewStatsService(oauth, cacheClient, statsRepo, logger)
}
