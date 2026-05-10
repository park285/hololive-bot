package runtime

import (
	"context"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
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
		cacheService:  cacheService,
		scheduler:     scheduler,
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
	registryVersion, registryVersionTrusted, registryVersionErr := r.channelRegistryVersion(ctx)
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

	cacheAlarmChannelIDs, cacheErr := r.cacheService.SMembers(ctx, sharedalarmkeys.AlarmChannelRegistryKey)
	var alarmChannelIDs []string
	candidateFromCache := false
	switch {
	case cacheErr == nil && len(cacheAlarmChannelIDs) > 0:
		r.lastNonEmptyCacheAt = now
		alarmChannelIDs = cacheAlarmChannelIDs
		candidateFromCache = true
	case cacheErr == nil && len(cacheAlarmChannelIDs) == 0:
		if !r.lastNonEmptyCacheAt.IsZero() && now.Sub(r.lastNonEmptyCacheAt) < youtubePollTargetEmptyCacheGracePeriod {
			if hasYouTubePollTargets(r.lastResolvedTargets) {
				if operational.changed {
					targets := r.lastResolvedTargets
					targets.StatsChannelIDs = communityshorts.EnabledChannelIDs(operationalChannels)
					if !equalYouTubePollTargets(r.lastResolvedTargets, targets) {
						r.applyResolvedTargets(targets)
					}
				}
				return
			}
			alarmChannelIDs = cacheAlarmChannelIDs
			candidateFromCache = true
		} else {
			dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
			if dbErr != nil {
				if r.logger != nil {
					r.logger.Warn("Failed to refresh YouTube poll targets from DB fallback",
						slog.Any("error", dbErr))
				}
				return
			}
			alarmChannelIDs = dbAlarmChannelIDs
		}
	default:
		if r.logger != nil {
			r.logger.Warn("Failed to refresh YouTube poll targets from cache",
				slog.Any("error", cacheErr))
		}
		dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
		if dbErr != nil {
			if r.logger != nil {
				r.logger.Warn("Failed to refresh YouTube poll targets from DB fallback",
					slog.Any("error", dbErr))
			}
			return
		}
		alarmChannelIDs = dbAlarmChannelIDs
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

func (r *youTubePollTargetRefresher) channelRegistryVersion(ctx context.Context) (version int64, trusted bool, err error) {
	defer func() {
		if recovered := recover(); recovered != nil {
			version = 0
			trusted = false
			err = nil
		}
	}()

	exists, err := r.cacheService.Exists(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey)
	if err != nil {
		return 0, false, err
	}
	if !exists {
		return 0, false, nil
	}

	if err := r.cacheService.Get(ctx, sharedalarmkeys.AlarmChannelRegistryVersionKey, &version); err != nil {
		return 0, false, err
	}
	if version <= 0 {
		return 0, false, nil
	}

	return version, true, nil
}

func (r *youTubePollTargetRefresher) applyResolvedTargets(targets youtubePollTargets) {
	if r == nil || r.scheduler == nil {
		return
	}
	for _, registration := range r.registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}
		if !registration.HasExplicitChannelIDs {
			continue
		}

		updated := registration
		switch registration.TargetGroup {
		case providers.ChannelTargetGroupStats:
			updated.ChannelIDs = append([]string(nil), targets.StatsChannelIDs...)
		case providers.ChannelTargetGroupGlobal:
			updated.ChannelIDs = append([]string(nil), registration.ChannelIDs...)
		default:
			updated.ChannelIDs = append([]string(nil), targets.NotificationChannelIDs...)
		}

		sync := updated.ToTargetSync()
		if registration.TargetGroup == providers.ChannelTargetGroupNotification {
			sync.ForceImmediateFirstRun = true
		}
		r.scheduler.SyncPollerTargets(sync)
	}
	r.lastResolvedTargets = targets
}
