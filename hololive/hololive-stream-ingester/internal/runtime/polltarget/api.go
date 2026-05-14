package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/database"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"gorm.io/gorm"
)

type Targets = youtubePollTargets
type TieredTargets = youtubeTieredPollTargets
type Refresher = youTubePollTargetRefresher
type SchedulerSyncer = youTubePollSchedulerSyncer

func Resolve(
	ctx context.Context,
	cacheService cache.Client,
	postgresService database.Client,
	operationalChannels []communityShortsOperationalChannel,
	logger *slog.Logger,
) (Targets, error) {
	return resolveYouTubePollTargets(ctx, cacheService, postgresService, operationalChannels, logger)
}

func LoadAlarmChannelIDs(ctx context.Context, postgresService database.Client) ([]string, error) {
	return loadAlarmChannelIDs(ctx, postgresService)
}

func NewRefresher(
	cacheService cache.Client,
	scheduler *poller.Scheduler,
	registrations []providers.ChannelPollerRegistration,
	operationalChannels []communityShortsOperationalChannel,
	loadAlarmChannelIDs func(context.Context) ([]string, error),
	logger *slog.Logger,
) *Refresher {
	return newYouTubePollTargetRefresher(cacheService, scheduler, registrations, operationalChannels, loadAlarmChannelIDs, logger)
}

func NewSchedulerSyncer(
	scheduler *poller.Scheduler,
	registrations []providers.ChannelPollerRegistration,
	tieringDB *gorm.DB,
) *SchedulerSyncer {
	return &youTubePollSchedulerSyncer{
		scheduler:     scheduler,
		registrations: registrations,
		tieringDB:     tieringDB,
	}
}

func (r *Refresher) WithTieringDB(db *gorm.DB) *Refresher {
	return r.withTieringDB(db)
}

func (r *Refresher) WithOperationalChannelLoader(
	loadOperationalChannels func(context.Context) ([]communityShortsOperationalChannel, error),
) *Refresher {
	return r.withOperationalChannelLoader(loadOperationalChannels)
}

func ClassifyByActivity(ctx context.Context, db *gorm.DB, targets Targets, now time.Time) (TieredTargets, error) {
	return classifyYouTubePollTargetsByActivity(ctx, db, targets, now)
}

func ResolveFromRegistrations(registrations []providers.ChannelPollerRegistration) Targets {
	return resolveYouTubePollTargetsFromRegistrations(registrations)
}

func MergeUniqueChannelIDs(channelIDSets ...[]string) []string {
	return mergeUniqueChannelIDs(channelIDSets...)
}

func HasTieredNotificationRegistration(registrations []providers.ChannelPollerRegistration) bool {
	return hasTieredNotificationRegistration(registrations)
}
