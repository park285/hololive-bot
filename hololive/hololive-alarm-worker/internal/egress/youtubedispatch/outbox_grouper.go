package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/park285/shared-go/postgres/pgxdb"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type OutboxGrouper struct {
	cache             alarmstore.SubscriberCache
	lookupSubscribers func(context.Context, string, []string, domain.AlarmType) (map[string][]string, error)
	logger            *slog.Logger
	config            Config
}

func newOutboxGrouper(db pgxdb.Querier, cacheClient alarmstore.SubscriberCache, logger *slog.Logger, config *Config) *OutboxGrouper {
	if logger == nil {
		logger = slog.Default()
	}

	resolver := alarmstore.NewSubscriberResolver(cacheClient, db)

	return &OutboxGrouper{
		cache:             cacheClient,
		lookupSubscribers: resolver.ResolveEventSubscribersByTitle,
		logger:            logger,
		config:            *config,
	}
}
