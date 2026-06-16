package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

const youtubePollTargetRefreshInterval = 5 * time.Second
const youtubePollTargetEmptyCacheGracePeriod = 30 * time.Second
const youtubePollTargetCacheOnlyAdditionGracePeriod = 30 * time.Second
const youtubePollTargetTieringRefreshInterval = time.Minute

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
	lastTieringRefreshAt       time.Time
	cacheOnlyFirstSeen         map[string]time.Time
	initialJitter              time.Duration
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
			logger:        logger,
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

func (r *youTubePollTargetRefresher) withTieringDB(pool *pgxpool.Pool) *youTubePollTargetRefresher {
	if r == nil || r.schedulerSyncer == nil {
		return r
	}
	r.schedulerSyncer.tieringDB = pool
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

func (r *youTubePollTargetRefresher) withInitialJitter(jitter time.Duration) *youTubePollTargetRefresher {
	if r == nil || jitter <= 0 {
		return r
	}
	r.initialJitter = jitter
	return r
}

func (r *youTubePollTargetRefresher) Start(ctx context.Context) {
	ticker := time.NewTicker(youtubePollTargetRefreshInterval)
	defer ticker.Stop()

	if !r.waitInitialJitter(ctx) {
		return
	}
	r.refresh(ctx)
	r.runRefreshLoop(ctx, ticker)
}

func (r *youTubePollTargetRefresher) waitInitialJitter(ctx context.Context) bool {
	if r == nil || r.initialJitter <= 0 {
		return true
	}
	timer := time.NewTimer(r.initialJitter)
	defer timer.Stop()
	select {
	case <-ctx.Done():
		return false
	case <-timer.C:
		return true
	}
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
	if r.reuseTargetsIfRegistryUnchanged(ctx, registry, operational) {
		return
	}

	now := r.now()
	candidate, ok := r.resolveAlarmTargetCandidate(ctx, now)
	if !ok {
		return
	}
	if r.reuseTargetsForEmptyCacheCandidate(ctx, candidate, operational) {
		return
	}

	targets, ok := r.resolveTargetsWithCacheValidation(ctx, now, operational.channels, candidate.ids, candidate.fromCache)
	if !ok {
		return
	}
	r.finishRefresh(ctx, targets, operational, registry, candidate.fromCache)
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
	ctx context.Context,
	registry registryVersionSnapshot,
	operational operationalChannelResolution,
) bool {
	if !registry.trusted || r.lastChannelRegistryVersion != registry.version || !hasYouTubePollTargets(r.lastResolvedTargets) {
		return false
	}
	targets := r.lastResolvedTargets
	targetsChanged := false
	if operational.changed {
		targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operational.channels)
		targetsChanged = !equalYouTubePollTargets(r.lastResolvedTargets, targets)
	}
	if targetsChanged || r.shouldRefreshTieredTargets() {
		r.applyResolvedTargets(ctx, targets)
		return true
	}
	return true
}

func (r *youTubePollTargetRefresher) applyStatsTargetRefreshIfChanged(ctx context.Context, operational operationalChannelResolution) {
	if !operational.changed {
		return
	}

	targets := r.lastResolvedTargets
	targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operational.channels)
	if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
		r.applyResolvedTargets(ctx, targets)
	}
}

func (r *youTubePollTargetRefresher) shouldRefreshTieredTargets() bool {
	if r == nil || r.schedulerSyncer == nil || !r.schedulerSyncer.hasTieredRegistrations() {
		return false
	}
	now := r.now()
	if !r.lastTieringRefreshAt.IsZero() && now.Sub(r.lastTieringRefreshAt) < youtubePollTargetTieringRefreshInterval {
		return false
	}
	r.lastTieringRefreshAt = now
	return true
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
	ctx context.Context,
	candidate alarmTargetCandidate,
	operational operationalChannelResolution,
) bool {
	if !candidate.fromCache || len(candidate.ids) != 0 || !hasYouTubePollTargets(r.lastResolvedTargets) {
		return false
	}
	r.applyStatsTargetRefreshIfChanged(ctx, operational)
	return true
}

func (r *youTubePollTargetRefresher) finishRefresh(
	ctx context.Context,
	targets youtubePollTargets,
	operational operationalChannelResolution,
	registry registryVersionSnapshot,
	candidateFromCache bool,
) {
	if candidateFromCache && registry.trusted {
		r.lastChannelRegistryVersion = registry.version
	}
	targetsChanged := !equalYouTubePollTargets(r.lastResolvedTargets, targets)
	tieringRefreshDue := r.shouldRefreshTieredTargets()
	if r.logger != nil && (operational.changed || targetsChanged) {
		r.logger.Info("youtube_poll_target_operational_channels_refreshed",
			slog.Int("operational_channel_count", len(operational.channels)),
			slog.Int("notification_target_channels", len(targets.NotificationChannelIDs)),
			slog.Int("stats_target_channels", len(targets.StatsChannelIDs)),
			slog.Bool("fallback_used", operational.fallbackUsed),
		)
	}
	if targetsChanged || tieringRefreshDue {
		r.applyResolvedTargets(ctx, targets)
	}
}

func (r *youTubePollTargetRefresher) applyResolvedTargets(ctx context.Context, targets youtubePollTargets) {
	if r == nil || r.schedulerSyncer == nil {
		return
	}
	r.schedulerSyncer.SyncAt(ctx, targets, r.now())
	if r.schedulerSyncer.hasTieredRegistrations() {
		r.lastTieringRefreshAt = r.now()
	}
	r.lastResolvedTargets = targets
}
