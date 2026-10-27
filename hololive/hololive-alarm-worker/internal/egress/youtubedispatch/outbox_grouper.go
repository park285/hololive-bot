package youtubedispatch

import (
	"context"
	"log/slog"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type OutboxGrouper struct {
	cache alarmstore.SubscriberCache
	// lookupSubscribers는 같은 채널·알림 종류의 여러 제목을 구독 조회 한 번으로 처리한다.
	lookupSubscribers func(context.Context, string, []string, domain.AlarmType) (map[string][]string, error)
	logger            *slog.Logger
	config            Config
}

func newOutboxGrouper(db dbx.Querier, cacheClient alarmstore.SubscriberCache, logger *slog.Logger, config *Config) *OutboxGrouper {
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
