package delivery

import (
	"log/slog"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type OutboxGrouper struct {
	db     *gorm.DB
	cache  cache.Client
	logger *slog.Logger
	config Config
}

func newOutboxGrouper(db *gorm.DB, cacheClient cache.Client, logger *slog.Logger, config Config) *OutboxGrouper {
	if logger == nil {
		logger = slog.Default()
	}
	return &OutboxGrouper{
		db:     db,
		cache:  cacheClient,
		logger: logger,
		config: config,
	}
}

func (g *OutboxGrouper) subscriberLookupParallelism() int {
	if g == nil || g.config.SubscriberLookupParallelism <= 0 {
		return DefaultConfig().SubscriberLookupParallelism
	}
	return g.config.SubscriberLookupParallelism
}
