package runtime

import (
	"context"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	communityshorts "github.com/kapu/hololive-stream-ingester/internal/communityshorts"
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

	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			r.refresh(ctx)
		}
	}
}

func (r *youTubePollTargetRefresher) refresh(ctx context.Context) {
	if r == nil || r.cacheService == nil || r.scheduler == nil {
		return
	}

	nowFn := r.timeNow
	if nowFn == nil {
		nowFn = time.Now
	}
	now := nowFn()
	operational, err := r.resolveOperationalChannels(ctx)
	if err != nil {
		if r.logger != nil {
			r.logger.Warn("Failed to refresh operational channels for YouTube poll targets",
				slog.Any("error", err))
		}
		return
	}
	operationalChannels := operational.channels
	if operational.fallbackUsed {
		if !r.lastOperationalFallback && r.logger != nil {
			r.logger.Warn("Using last known operational channels for YouTube poll targets",
				slog.Int("operational_channel_count", len(operationalChannels)))
		}
		r.lastOperationalFallback = true
	} else {
		if r.lastOperationalFallback && r.logger != nil {
			r.logger.Info("Recovered operational channel refresh for YouTube poll targets",
				slog.Int("operational_channel_count", len(operationalChannels)))
		}
		r.lastOperationalFallback = false
	}
	registryVersion, registryVersionTrusted, registryVersionErr := r.registryVersionSource.Version(ctx)
	if registryVersionErr != nil && r.logger != nil {
		r.logger.Warn("Failed to read alarm channel registry version",
			slog.Any("error", registryVersionErr))
	}
	if registryVersionTrusted &&
		r.lastChannelRegistryVersion == registryVersion &&
		hasYouTubePollTargets(r.lastResolvedTargets) {
		if operational.changed {
			targets := r.lastResolvedTargets
			targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operationalChannels)
			if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
				r.applyResolvedTargets(targets)
			}
		}
		return
	}

	alarmChannelIDs, candidateFromCache, lastNonEmptyCacheAt, ok := r.targetResolver.ResolveAlarmChannelIDs(ctx, now, r.lastNonEmptyCacheAt)
	r.lastNonEmptyCacheAt = lastNonEmptyCacheAt
	if !ok {
		return
	}
	if candidateFromCache && len(alarmChannelIDs) == 0 && hasYouTubePollTargets(r.lastResolvedTargets) {
		if operational.changed {
			targets := r.lastResolvedTargets
			targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operationalChannels)
			if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
				r.applyResolvedTargets(targets)
			}
		}
		return
	}

	targets, ok := r.resolveTargetsWithCacheValidation(
		ctx,
		now,
		operationalChannels,
		alarmChannelIDs,
		candidateFromCache,
	)
	if !ok {
		return
	}
	if candidateFromCache && registryVersionTrusted {
		r.lastChannelRegistryVersion = registryVersion
	}
	targetsChanged := !equalYouTubePollTargets(r.lastResolvedTargets, targets)
	if r.logger != nil && (operational.changed || targetsChanged) {
		r.logger.Info("youtube_poll_target_operational_channels_refreshed",
			slog.Int("operational_channel_count", len(operationalChannels)),
			slog.Int("notification_target_channels", len(targets.NotificationChannelIDs)),
			slog.Int("stats_target_channels", len(targets.StatsChannelIDs)),
			slog.Bool("fallback_used", operational.fallbackUsed),
		)
	}
	if !targetsChanged {
		return
	}
	r.applyResolvedTargets(targets)
}

func (r *youTubePollTargetRefresher) applyResolvedTargets(targets youtubePollTargets) {
	if r == nil || r.schedulerSyncer == nil {
		return
	}
	r.schedulerSyncer.Sync(targets)
	r.lastResolvedTargets = targets
}
