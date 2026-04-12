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
const youtubePollTargetCacheOnlyAdditionGracePeriod = 30 * time.Second

type youTubePollTargetRefresher struct {
	cacheService        cache.Client
	scheduler           *poller.Scheduler
	registrations       []providers.ChannelPollerRegistration
	operationalChannels []communityShortsOperationalChannel
	loadAlarmChannelIDs func(context.Context) ([]string, error)
	lastNonEmptyCacheAt time.Time
	lastResolvedTargets youtubePollTargets
	cacheOnlyFirstSeen  map[string]time.Time
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
		cacheOnlyFirstSeen:  make(map[string]time.Time),
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
	targets := candidateTargets
	if candidateFromCache {
		if r.cacheOnlyFirstSeen == nil {
			r.cacheOnlyFirstSeen = make(map[string]time.Time)
		}
		removed := removedChannelIDs(r.lastResolvedTargets.NotificationChannelIDs, candidateTargets.NotificationChannelIDs)
		added := addedChannelIDs(r.lastResolvedTargets.NotificationChannelIDs, candidateTargets.NotificationChannelIDs)
		needsDBValidation := len(removed) > 0 || len(added) > 0 || len(r.cacheOnlyFirstSeen) > 0
		if !needsDBValidation {
			observeYouTubePollTargetValidation("skipped")
		} else {
			dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
			if dbErr != nil {
				observeYouTubePollTargetValidation("failed")
				if r.logger != nil {
					r.logger.Warn("Failed to validate YouTube poll targets from DB",
						slog.Any("error", dbErr))
				}
				return
			}
			dbTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(dbAlarmChannelIDs, r.operationalChannels)
			observeYouTubePollTargetValidation("validated")
			cacheOnlyAdditions := diffChannelIDs(candidateTargets.NotificationChannelIDs, dbTargets.NotificationChannelIDs)
			trackCacheOnlyAdditions(now, cacheOnlyAdditions, r.cacheOnlyFirstSeen)
			allowedCacheOnly, expiredCacheOnly := filterGracefulCacheOnlyAdditions(
				now,
				cacheOnlyAdditions,
				r.cacheOnlyFirstSeen,
				youtubePollTargetCacheOnlyAdditionGracePeriod,
			)
			targets = dbTargets
			targets.NotificationChannelIDs = unionChannelIDs(targets.NotificationChannelIDs, allowedCacheOnly)
			clearExpiredOrResolvedCacheOnly(r.cacheOnlyFirstSeen, dbTargets.NotificationChannelIDs, candidateTargets.NotificationChannelIDs)
			if r.logger != nil {
				r.logger.Info("youtube_poll_target_refresh_db_validated",
					slog.Int("previous_notification_channels", len(r.lastResolvedTargets.NotificationChannelIDs)),
					slog.Int("candidate_notification_channels", len(candidateTargets.NotificationChannelIDs)),
					slog.Int("db_notification_channels", len(dbTargets.NotificationChannelIDs)),
					slog.Int("allowed_cache_only_additions", len(allowedCacheOnly)),
					slog.Int("expired_cache_only_additions", len(expiredCacheOnly)),
					slog.Int("removed_candidate_channels", len(removed)),
				)
				}
			}
		}
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

func removedChannelIDs(prev, next []string) []string {
	return diffChannelIDs(prev, next)
}

func addedChannelIDs(prev, next []string) []string {
	return diffChannelIDs(next, prev)
}

func unionChannelIDs(left, right []string) []string {
	return mergeUniqueChannelIDs(left, right)
}

func trackCacheOnlyAdditions(now time.Time, additions []string, state map[string]time.Time) {
	if state == nil {
		return
	}
	for _, channelID := range additions {
		if channelID == "" {
			continue
		}
		if _, exists := state[channelID]; !exists {
			state[channelID] = now
		}
	}
}

func clearExpiredOrResolvedCacheOnly(
	state map[string]time.Time,
	authoritative []string,
	candidate []string,
) {
	if state == nil {
		return
	}

	authoritativeSet := make(map[string]struct{}, len(authoritative))
	for _, channelID := range authoritative {
		if channelID == "" {
			continue
		}
		authoritativeSet[channelID] = struct{}{}
	}
	candidateSet := make(map[string]struct{}, len(candidate))
	for _, channelID := range candidate {
		if channelID == "" {
			continue
		}
		candidateSet[channelID] = struct{}{}
	}

	for channelID := range state {
		if _, stillCandidate := candidateSet[channelID]; !stillCandidate {
			delete(state, channelID)
			continue
		}
		if _, nowAuthoritative := authoritativeSet[channelID]; nowAuthoritative {
			delete(state, channelID)
		}
	}
}

func filterGracefulCacheOnlyAdditions(
	now time.Time,
	additions []string,
	state map[string]time.Time,
	grace time.Duration,
) (allowed []string, expired []string) {
	if state == nil {
		return nil, nil
	}

	for _, channelID := range additions {
		firstSeenAt, exists := state[channelID]
		if !exists {
			continue
		}
		if now.Sub(firstSeenAt) <= grace {
			allowed = append(allowed, channelID)
			continue
		}
		expired = append(expired, channelID)
	}
	return allowed, expired
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
