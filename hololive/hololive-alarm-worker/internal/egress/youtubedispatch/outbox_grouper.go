package youtubedispatch

import (
	"context"
	"log/slog"

	dispatchstate "github.com/kapu/hololive-alarm-worker/internal/service/youtube/outbox/dispatchstate"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

type OutboxGrouper struct {
	cache cache.Client
	// lookupSubscribers는 같은 채널·알림 종류의 여러 제목을 구독 조회 한 번으로 처리한다.
	lookupSubscribers func(context.Context, string, []string, domain.AlarmType) (map[string][]string, error)
	logger            *slog.Logger
	config            dispatchstate.Config
}

func newOutboxGrouper(db dbx.Querier, cacheClient cache.Client, logger *slog.Logger, config *dispatchstate.Config) *OutboxGrouper {
	if logger == nil {
		logger = slog.Default()
	}

	resolver := sharedalarm.NewSubscriberResolver(cacheClient, db)

	return &OutboxGrouper{
		cache:             cacheClient,
		lookupSubscribers: resolver.ResolveEventSubscribersByTitle,
		logger:            logger,
		config:            *config,
	}
}
