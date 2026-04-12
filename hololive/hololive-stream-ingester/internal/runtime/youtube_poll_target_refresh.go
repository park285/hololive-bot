package runtime

import (
	"context"
	"log/slog"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

const youtubePollTargetRefreshInterval = 5 * time.Second
const youtubePollTargetEmptyCacheGracePeriod = 30 * time.Second

type youTubePollTargetRefresher struct {
	cacheService        cache.Client
	scheduler           *poller.Scheduler
	registrations       []providers.ChannelPollerRegistration
	operationalChannels []communityShortsOperationalChannel
	loadAlarmChannelIDs func(context.Context) ([]string, error)
	lastNonEmptyCacheAt time.Time
	lastResolvedTargets youtubePollTargets
	timeNow             func() time.Time
	logger              *slog.Logger
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

	return &youTubePollTargetRefresher{
		cacheService:        cacheService,
		scheduler:           scheduler,
		registrations:       append([]providers.ChannelPollerRegistration(nil), registrations...),
		operationalChannels: append([]communityShortsOperationalChannel(nil), operationalChannels...),
		loadAlarmChannelIDs: loadAlarmChannelIDs,
		lastResolvedTargets: resolveYouTubePollTargetsFromRegistrations(registrations),
		timeNow:             time.Now,
		logger:              logger,
	}
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

	candidateTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(alarmChannelIDs, r.operationalChannels)
	if !candidateFromCache || !shouldValidateTargetShrink(r.lastResolvedTargets, candidateTargets) {
		observeYouTubePollTargetShrinkValidation("skipped")
	} else {
		if r.logger != nil {
			r.logger.Warn("YouTube poll targets shrinking; validating against DB",
				slog.Int("previous_notification_channels", len(r.lastResolvedTargets.NotificationChannelIDs)),
				slog.Int("candidate_notification_channels", len(candidateTargets.NotificationChannelIDs)),
			)
		}
		dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
		if dbErr != nil {
			observeYouTubePollTargetShrinkValidation("failed")
			if r.logger != nil {
				r.logger.Warn("Failed to validate YouTube poll target shrink from DB",
					slog.Any("error", dbErr))
			}
			return
		}
		validatedTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(dbAlarmChannelIDs, r.operationalChannels)
		observeYouTubePollTargetShrinkValidation("validated")
		if r.logger != nil {
			r.logger.Info("youtube_poll_target_refresh_cache_shrink_validated",
				slog.Int("previous_notification_channels", len(r.lastResolvedTargets.NotificationChannelIDs)),
				slog.Int("candidate_notification_channels", len(candidateTargets.NotificationChannelIDs)),
				slog.Int("validated_notification_channels", len(validatedTargets.NotificationChannelIDs)),
			)
		}
		candidateTargets = validatedTargets
	}

	targets := candidateTargets
	if equalYouTubePollTargets(r.lastResolvedTargets, targets) {
		return
	}
	for _, registration := range r.registrations {
		if registration.Poller == nil || registration.Interval <= 0 {
			continue
		}

		updated := registration
		switch registration.TargetGroup {
		case providers.ChannelTargetGroupStats:
			updated.ChannelIDs = append([]string(nil), targets.StatsChannelIDs...)
		default:
			updated.ChannelIDs = append([]string(nil), targets.NotificationChannelIDs...)
		}

		sync := updated.ToTargetSync()
		if registration.TargetGroup != providers.ChannelTargetGroupStats {
			sync.ForceImmediateFirstRun = true
		}
		r.scheduler.SyncPollerTargets(sync)
	}
	r.lastResolvedTargets = targets
}

func resolveYouTubePollTargetsFromRegistrations(registrations []providers.ChannelPollerRegistration) youtubePollTargets {
	var notificationChannelIDs []string
	var statsChannelIDs []string

	for _, registration := range registrations {
		switch registration.TargetGroup {
		case providers.ChannelTargetGroupStats:
			statsChannelIDs = mergeUniqueChannelIDs(statsChannelIDs, registration.ChannelIDs)
		default:
			notificationChannelIDs = mergeUniqueChannelIDs(notificationChannelIDs, registration.ChannelIDs)
		}
	}

	return youtubePollTargets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
	}
}

func hasYouTubePollTargets(targets youtubePollTargets) bool {
	return len(targets.NotificationChannelIDs) > 0 || len(targets.StatsChannelIDs) > 0
}

func shouldValidateTargetShrink(prev, next youtubePollTargets) bool {
	return len(next.NotificationChannelIDs) < len(prev.NotificationChannelIDs)
}

func equalYouTubePollTargets(a, b youtubePollTargets) bool {
	return sameChannelIDSet(a.NotificationChannelIDs, b.NotificationChannelIDs) &&
		sameChannelIDSet(a.StatsChannelIDs, b.StatsChannelIDs)
}

func sameChannelIDSet(left, right []string) bool {
	if len(left) != len(right) {
		return false
	}

	counts := make(map[string]int, len(left))
	for _, channelID := range left {
		counts[channelID]++
	}
	for _, channelID := range right {
		counts[channelID]--
		if counts[channelID] < 0 {
			return false
		}
	}
	for _, count := range counts {
		if count != 0 {
			return false
		}
	}
	return true
}
