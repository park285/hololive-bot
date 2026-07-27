package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	pollscheduler "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

func Resolve(
	ctx context.Context,
	cacheService cache.Client,
	postgresService database.Client,
	operationalChannels []communityshorts.OperationalChannel,
	logger *slog.Logger,
) (Targets, error) {
	return resolveYouTubePollTargets(ctx, cacheService, postgresService, operationalChannels, logger)
}

func LoadAlarmChannelIDs(ctx context.Context, postgresService database.Client) ([]string, error) {
	return loadAlarmChannelIDs(ctx, postgresService)
}

func NewRefresher(
	cacheService cache.Client,
	scheduler *pollscheduler.Scheduler,
	registrations []providers.ChannelPollerRegistration,
	operationalChannels []communityshorts.OperationalChannel,
	loadAlarmChannelIDs func(context.Context) ([]string, error),
	logger *slog.Logger,
) *Refresher {
	return newYouTubePollTargetRefresher(cacheService, scheduler, registrations, operationalChannels, loadAlarmChannelIDs, logger)
}

func NewSchedulerSyncer(
	scheduler *pollscheduler.Scheduler,
	registrations []providers.ChannelPollerRegistration,
	tieringDB *pgxpool.Pool,
) *SchedulerSyncer {
	return &SchedulerSyncer{
		scheduler:     scheduler,
		registrations: registrations,
		tieringDB:     tieringDB,
	}
}

func (r *Refresher) WithTieringDB(pool *pgxpool.Pool) *Refresher {
	return r.withTieringDB(pool)
}

func (r *Refresher) WithOperationalChannelLoader(
	loadOperationalChannels func(context.Context) ([]communityshorts.OperationalChannel, error),
) *Refresher {
	return r.withOperationalChannelLoader(loadOperationalChannels)
}

func (r *Refresher) WithInitialJitter(jitter time.Duration) *Refresher {
	return r.withInitialJitter(jitter)
}

func ClassifyByActivity(ctx context.Context, pool *pgxpool.Pool, targets Targets, now time.Time) (TieredTargets, error) {
	return classifyYouTubePollTargetsByActivity(ctx, pool, targets, now)
}

func MergeUniqueChannelIDs(channelIDSets ...[]string) []string {
	return mergeUniqueChannelIDs(channelIDSets...)
}

func HasTieredNotificationRegistration(registrations []providers.ChannelPollerRegistration) bool {
	return hasTieredNotificationRegistration(registrations)
}
