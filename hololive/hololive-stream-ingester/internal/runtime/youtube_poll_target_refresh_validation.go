package runtime

import (
	"context"
	"log/slog"
	"time"
)

func (r *youTubePollTargetRefresher) resolveTargetsWithCacheValidation(
	ctx context.Context,
	now time.Time,
	operationalChannels []communityShortsOperationalChannel,
	alarmChannelIDs []string,
	candidateFromCache bool,
) (youtubePollTargets, bool) {
	candidateTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(alarmChannelIDs, operationalChannels)
	if !candidateFromCache {
		return candidateTargets, true
	}
	if r.cacheOnlyFirstSeen == nil {
		r.cacheOnlyFirstSeen = make(map[string]time.Time)
	}

	removed := removedChannelIDs(r.lastResolvedTargets.NotificationChannelIDs, candidateTargets.NotificationChannelIDs)
	added := addedChannelIDs(r.lastResolvedTargets.NotificationChannelIDs, candidateTargets.NotificationChannelIDs)
	trackCacheOnlyAdditions(now, added, r.cacheOnlyFirstSeen)
	clearExpiredOrResolvedCacheOnly(r.cacheOnlyFirstSeen, nil, candidateTargets.NotificationChannelIDs)
	_, expiredCacheOnly := filterGracefulCacheOnlyAdditions(
		now,
		candidateTargets.NotificationChannelIDs,
		r.cacheOnlyFirstSeen,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	)
	needsDBValidation := len(removed) > 0 || hasPendingCacheOnlyValidation(
		now,
		candidateTargets.NotificationChannelIDs,
		r.cacheOnlyFirstSeen,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	)
	if !needsDBValidation {
		targets := targetsWithoutExpiredCacheOnly(candidateTargets, expiredCacheOnly)
		observeYouTubePollTargetValidation("skipped")
		return targets, true
	}

	return r.validateTargetsAgainstDB(ctx, now, operationalChannels, candidateTargets, removed)
}

func targetsWithoutExpiredCacheOnly(targets youtubePollTargets, expired []string) youtubePollTargets {
	targets.NotificationChannelIDs = diffChannelIDs(targets.NotificationChannelIDs, expired)
	return targets
}

func (r *youTubePollTargetRefresher) validateTargetsAgainstDB(
	ctx context.Context,
	now time.Time,
	operationalChannels []communityShortsOperationalChannel,
	candidateTargets youtubePollTargets,
	removed []string,
) (youtubePollTargets, bool) {
	dbAlarmChannelIDs, dbErr := r.loadAlarmChannelIDs(ctx)
	if dbErr != nil {
		observeYouTubePollTargetValidation("failed")
		if r.logger != nil {
			r.logger.Warn("Failed to validate YouTube poll targets from DB",
				slog.Any("error", dbErr))
		}
		return youtubePollTargets{}, false
	}

	dbTargets := resolveYouTubePollTargetsFromAlarmChannelIDs(dbAlarmChannelIDs, operationalChannels)
	observeYouTubePollTargetValidation("validated")
	cacheOnlyAdditions := diffChannelIDs(candidateTargets.NotificationChannelIDs, dbTargets.NotificationChannelIDs)
	trackCacheOnlyAdditions(now, cacheOnlyAdditions, r.cacheOnlyFirstSeen)
	allowedCacheOnly, expiredCacheOnly := filterGracefulCacheOnlyAdditions(
		now,
		cacheOnlyAdditions,
		r.cacheOnlyFirstSeen,
		youtubePollTargetCacheOnlyAdditionGracePeriod,
	)
	targets := dbTargets
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
	return targets, true
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

	authoritativeSet := channelIDSet(authoritative)
	candidateSet := channelIDSet(candidate)

	for channelID := range state {
		if !hasChannelID(candidateSet, channelID) || hasChannelID(authoritativeSet, channelID) {
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

func hasPendingCacheOnlyValidation(
	now time.Time,
	candidate []string,
	state map[string]time.Time,
	grace time.Duration,
) bool {
	if len(state) == 0 || len(candidate) == 0 {
		return false
	}

	candidateSet := channelIDSet(candidate)

	for channelID, firstSeenAt := range state {
		if hasChannelID(candidateSet, channelID) && now.Sub(firstSeenAt) <= grace {
			return true
		}
	}

	return false
}

func channelIDSet(channelIDs []string) map[string]struct{} {
	set := make(map[string]struct{}, len(channelIDs))
	for _, channelID := range channelIDs {
		if channelID != "" {
			set[channelID] = struct{}{}
		}
	}
	return set
}

func hasChannelID(set map[string]struct{}, channelID string) bool {
	_, ok := set[channelID]
	return ok
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
