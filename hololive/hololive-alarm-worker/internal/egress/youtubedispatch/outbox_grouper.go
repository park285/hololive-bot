package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type OutboxGrouper struct {
	cache             cache.Client
	lookupSubscribers func(context.Context, string, domain.AlarmType) ([]string, error)
	logger            *slog.Logger
	config            dispatchstate.Config
}

func newOutboxGrouper(db dbx.Querier, cacheClient cache.Client, logger *slog.Logger, config *dispatchstate.Config) *OutboxGrouper {
	if logger == nil {
		logger = slog.Default()
	}

	return &OutboxGrouper{
		cache: cacheClient,
		lookupSubscribers: func(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
			return sharedalarm.ResolveChannelSubscribersByType(ctx, cacheClient, db, channelID, alarmType)
		},
		logger: logger,
		config: *config,
	}
}

func (g *OutboxGrouper) subscriberLookupParallelism() int {
	if g == nil || g.config.SubscriberLookupParallelism <= 0 {
		return dispatchstate.DefaultConfig().SubscriberLookupParallelism
	}

	return g.config.SubscriberLookupParallelism
}
