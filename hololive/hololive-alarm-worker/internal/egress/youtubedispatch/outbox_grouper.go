package youtubedispatch

import (
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	dispatchstate "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/dispatchstate"
)

type OutboxGrouper struct {
	db     dbx.Querier
	cache  cache.Client
	logger *slog.Logger
	config dispatchstate.Config
}

func newOutboxGrouper(db dbx.Querier, cacheClient cache.Client, logger *slog.Logger, config *dispatchstate.Config) *OutboxGrouper {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxGrouper{
		db:     db,
		cache:  cacheClient,
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
