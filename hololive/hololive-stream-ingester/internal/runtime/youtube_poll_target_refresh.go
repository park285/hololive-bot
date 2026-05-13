package runtime

import (
	"context"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
	"gorm.io/gorm"
)

const youtubePollTargetRefreshInterval = 5 * time.Second
const youtubePollTargetEmptyCacheGracePeriod = 30 * time.Second
const youtubePollTargetCacheOnlyAdditionGracePeriod = 30 * time.Second

type youTubePollTargetRefresher struct {
	cacheService               cache.Client
	scheduler                  *poller.Scheduler
	registryVersionSource      *youTubePollRegistryVersionSource
	targetResolver             *youTubePollTargetResolver
	schedulerSyncer            *youTubePollSchedulerSyncer
	registrations              []providers.ChannelPollerRegistration
	loadOperationalChannels    func(context.Context) ([]communityShortsOperationalChannel, error)
	lastOperationalChannels    []communityShortsOperationalChannel
	lastOperationalFallback    bool
	loadAlarmChannelIDs        func(context.Context) ([]string, error)
	lastNonEmptyCacheAt        time.Time
	lastResolvedTargets        youtubePollTargets
	lastChannelRegistryVersion int64
	cacheOnlyFirstSeen         map[string]time.Time
	timeNow                    func() time.Time
	logger                     *slog.Logger
}

func newYouTubePollTargetRefresher(
	cacheService cache.Client,
	scheduler *poller.Scheduler,
	registrations []providers.ChannelPollerRegistration,
	operationalChannels []communityShortsOperationalChannel,
	loadAlarmChannelIDs func(context.Context) ([]string, error),
	logger *slog.Logger,
) *youTubePollTargetRefresher {
	if cacheService == nil || scheduler == nil || len(registrations) == 0 || loadAlarmChannelIDs == nil {
		return nil
	}
	snapshot := append([]communityShortsOperationalChannel(nil), operationalChannels...)

	return &youTubePollTargetRefresher{
		cacheService: cacheService,
		scheduler:    scheduler,
		registryVersionSource: &youTubePollRegistryVersionSource{
			cacheService: cacheService,
		},
		targetResolver: &youTubePollTargetResolver{
			cacheService:        cacheService,
			loadAlarmChannelIDs: loadAlarmChannelIDs,
			logger:              logger,
		},
		schedulerSyncer: &youTubePollSchedulerSyncer{
			scheduler:     scheduler,
			registrations: append([]providers.ChannelPollerRegistration(nil), registrations...),
		},
		registrations: append([]providers.ChannelPollerRegistration(nil), registrations...),
		loadOperationalChannels: func(context.Context) ([]communityShortsOperationalChannel, error) {
			return append([]communityShortsOperationalChannel(nil), snapshot...), nil
		},
		lastOperationalChannels: snapshot,
		loadAlarmChannelIDs:     loadAlarmChannelIDs,
		lastResolvedTargets:     resolveYouTubePollTargetsFromRegistrations(registrations),
		cacheOnlyFirstSeen:      make(map[string]time.Time),
		timeNow:                 time.Now,
		logger:                  logger,
	}
}

func (r *youTubePollTargetRefresher) withTieringDB(db *gorm.DB) *youTubePollTargetRefresher {
	if r == nil || r.schedulerSyncer == nil {
		return r
	}
	r.schedulerSyncer.tieringDB = db
	return r
}

func (r *youTubePollTargetRefresher) withOperationalChannelLoader(
	loadOperationalChannels func(context.Context) ([]communityShortsOperationalChannel, error),
) *youTubePollTargetRefresher {
	if r == nil || loadOperationalChannels == nil {
		return r
	}
	r.loadOperationalChannels = loadOperationalChannels
	return r
}

func (r *youTubePollTargetRefresher) Start(ctx context.Context) {
	ticker := time.NewTicker(youtubePollTargetRefreshInterval)
	defer ticker.Stop()

	r.refresh(ctx)
	r.runRefreshLoop(ctx, ticker)
}

func (r *youTubePollTargetRefresher) runRefreshLoop(ctx context.Context, ticker *time.Ticker) {
	for {
		if !r.waitRefreshTick(ctx, ticker) {
			return
		}
		r.refresh(ctx)
	}
}

func (r *youTubePollTargetRefresher) waitRefreshTick(ctx context.Context, ticker *time.Ticker) bool {
	select {
	case <-ctx.Done():
		return false
	case <-ticker.C:
		return true
	}
}

func (r *youTubePollTargetRefresher) refresh(ctx context.Context) {
	if !r.readyToRefresh() {
		return
	}

	operational, ok := r.refreshOperationalChannels(ctx)
	if !ok {
		return
	}
	registry := r.readRegistryVersion(ctx)
	if r.reuseTargetsIfRegistryUnchanged(registry, operational) {
		return
	}

	now := r.now()
	candidate, ok := r.resolveAlarmTargetCandidate(ctx, now)
	if !ok {
		return
	}
	if r.reuseTargetsForEmptyCacheCandidate(candidate, operational) {
		return
	}

	targets, ok := r.resolveTargetsWithCacheValidation(ctx, now, operational.channels, candidate.ids, candidate.fromCache)
	if !ok {
		return
	}
	r.finishRefresh(targets, operational, registry, candidate.fromCache)
}

func (r *youTubePollTargetRefresher) readyToRefresh() bool {
	return r != nil && r.cacheService != nil && r.scheduler != nil
}

func (r *youTubePollTargetRefresher) now() time.Time {
	if r.timeNow != nil {
		return r.timeNow()
	}
	return time.Now()
}

func (r *youTubePollTargetRefresher) refreshOperationalChannels(ctx context.Context) (operationalChannelResolution, bool) {
	operational, err := r.resolveOperationalChannels(ctx)
	if err != nil {
		r.logOperationalRefreshError(err)
		return operationalChannelResolution{}, false
	}
	r.updateOperationalFallbackState(operational)
	return operational, true
}

func (r *youTubePollTargetRefresher) logOperationalRefreshError(err error) {
	if r.logger != nil {
		r.logger.Warn("Failed to refresh operational channels for YouTube poll targets", slog.Any("error", err))
	}
}

func (r *youTubePollTargetRefresher) updateOperationalFallbackState(operational operationalChannelResolution) {
	if operational.fallbackUsed {
		r.logOperationalFallbackStart(operational.channels)
		r.lastOperationalFallback = true
		return
	}

	r.logOperationalFallbackRecovered(operational.channels)
	r.lastOperationalFallback = false
}

func (r *youTubePollTargetRefresher) logOperationalFallbackStart(channels []communityShortsOperationalChannel) {
	if !r.lastOperationalFallback && r.logger != nil {
		r.logger.Warn("Using last known operational channels for YouTube poll targets",
			slog.Int("operational_channel_count", len(channels)))
	}
}

func (r *youTubePollTargetRefresher) logOperationalFallbackRecovered(channels []communityShortsOperationalChannel) {
	if r.lastOperationalFallback && r.logger != nil {
		r.logger.Info("Recovered operational channel refresh for YouTube poll targets",
			slog.Int("operational_channel_count", len(channels)))
	}
}

type registryVersionSnapshot struct {
	version int64
	trusted bool
}

func (r *youTubePollTargetRefresher) readRegistryVersion(ctx context.Context) registryVersionSnapshot {
	version, trusted, err := r.registryVersionSource.Version(ctx)
	if err != nil && r.logger != nil {
		r.logger.Warn("Failed to read alarm channel registry version", slog.Any("error", err))
	}
	return registryVersionSnapshot{version: version, trusted: trusted}
}

func (r *youTubePollTargetRefresher) reuseTargetsIfRegistryUnchanged(
	registry registryVersionSnapshot,
	operational operationalChannelResolution,
) bool {
	if !registry.trusted || r.lastChannelRegistryVersion != registry.version || !hasYouTubePollTargets(r.lastResolvedTargets) {
		return false
	}
	r.applyStatsTargetRefreshIfChanged(operational)
	return true
}

func (r *youTubePollTargetRefresher) applyStatsTargetRefreshIfChanged(operational operationalChannelResolution) {
	if !operational.changed {
		return
	}

	targets := r.lastResolvedTargets
	targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operational.channels)
	if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
		r.applyResolvedTargets(targets)
	}
}

type alarmTargetCandidate struct {
	ids       []string
	fromCache bool
}

func (r *youTubePollTargetRefresher) resolveAlarmTargetCandidate(
	ctx context.Context,
	now time.Time,
) (alarmTargetCandidate, bool) {
	ids, fromCache, lastNonEmptyCacheAt, ok := r.targetResolver.ResolveAlarmChannelIDs(ctx, now, r.lastNonEmptyCacheAt)
	r.lastNonEmptyCacheAt = lastNonEmptyCacheAt
	return alarmTargetCandidate{ids: ids, fromCache: fromCache}, ok
}

func (r *youTubePollTargetRefresher) reuseTargetsForEmptyCacheCandidate(
	candidate alarmTargetCandidate,
	operational operationalChannelResolution,
) bool {
	if !candidate.fromCache || len(candidate.ids) != 0 || !hasYouTubePollTargets(r.lastResolvedTargets) {
		return false
	}
	r.applyStatsTargetRefreshIfChanged(operational)
	return true
}

func (r *youTubePollTargetRefresher) finishRefresh(
	targets youtubePollTargets,
	operational operationalChannelResolution,
	registry registryVersionSnapshot,
	candidateFromCache bool,
) {
	if candidateFromCache && registry.trusted {
		r.lastChannelRegistryVersion = registry.version
	}
	targetsChanged := !equalYouTubePollTargets(r.lastResolvedTargets, targets)
	if r.logger != nil && (operational.changed || targetsChanged) {
		r.logger.Info("youtube_poll_target_operational_channels_refreshed",
			slog.Int("operational_channel_count", len(operational.channels)),
			slog.Int("notification_target_channels", len(targets.NotificationChannelIDs)),
			slog.Int("stats_target_channels", len(targets.StatsChannelIDs)),
			slog.Bool("fallback_used", operational.fallbackUsed),
		)
	}
	if targetsChanged {
		r.applyResolvedTargets(targets)
	}
}

func (r *youTubePollTargetRefresher) applyResolvedTargets(targets youtubePollTargets) {
	if r == nil || r.schedulerSyncer == nil {
		return
	}
	r.schedulerSyncer.Sync(targets)
	r.lastResolvedTargets = targets
}
