package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

const tieringQueryTimeout = 10 * time.Second

type youTubePollSchedulerSyncer struct {
	scheduler     *poller.Scheduler
	registrations []providers.ChannelPollerRegistration
	tieringDB     *pgxpool.Pool
	logger        *slog.Logger
}

func (s *youTubePollSchedulerSyncer) SyncAt(ctx context.Context, targets youtubePollTargets, now time.Time) {
	if s == nil || s.scheduler == nil {
		return
	}
	tieredTargets, hasTieredTargets := s.classifyTargetsForTieredRegistrations(ctx, targets, now)
	tieredSyncs := make(map[string][]poller.PollerTargetSync)
	for _, registration := range s.registrations {
		if !shouldSyncYouTubePollRegistration(registration) {
			continue
		}
		sync := youtubePollRegistrationTargetSync(registration, targets, tieredTargets, hasTieredTargets)
		if isTieredNotificationTargetGroup(registration.TargetGroup) {
			tieredSyncs[registration.Poller.Name()] = append(tieredSyncs[registration.Poller.Name()], sync)
			continue
		}
		s.scheduler.SyncPollerTargets(sync)
	}
	for _, syncs := range tieredSyncs {
		s.scheduler.SyncPollerTargetGroups(syncs)
	}
}

func (s *youTubePollSchedulerSyncer) classifyTargetsForTieredRegistrations(ctx context.Context, targets youtubePollTargets, now time.Time) (youtubeTieredPollTargets, bool) {
	if !s.hasTieredRegistrations() {
		return youtubeTieredPollTargets{}, false
	}
	if err := ctx.Err(); err != nil {
		s.logTieredClassifySkipped(err)
		return youtubeTieredPollTargets{}, false
	}
	classifyCtx, cancel := context.WithTimeout(ctx, tieringQueryTimeout)
	defer cancel()
	tieredTargets, err := classifyYouTubePollTargetsByActivity(classifyCtx, s.tieringDB, targets, now)
	if err != nil {
		if classifyCtx.Err() != nil {
			s.logTieredClassifySkipped(err)
		}
		return youtubeTieredPollTargets{}, false
	}
	return tieredTargets, true
}

func (s *youTubePollSchedulerSyncer) logTieredClassifySkipped(err error) {
	if s.logger != nil {
		s.logger.Warn("youtube_poll_target_tiered_classify_skipped", slog.Any("error", err))
	}
}

func (s *youTubePollSchedulerSyncer) hasTieredRegistrations() bool {
	return s != nil && hasTieredNotificationRegistration(s.registrations)
}

func shouldSyncYouTubePollRegistration(registration providers.ChannelPollerRegistration) bool {
	return registration.Poller != nil &&
		registration.Interval > 0 &&
		registration.HasExplicitChannelIDs
}

func youtubePollRegistrationTargetSync(
	registration providers.ChannelPollerRegistration,
	targets youtubePollTargets,
	tieredTargets youtubeTieredPollTargets,
	hasTieredTargets bool,
) poller.PollerTargetSync {
	updated := registration
	updated.ChannelIDs = youtubePollRegistrationChannelIDs(registration, targets, tieredTargets, hasTieredTargets)

	sync := updated.ToTargetSync()
	if isNotificationTargetGroup(registration.TargetGroup) {
		sync.ForceImmediateFirstRun = true
	}
	return sync
}

func youtubePollRegistrationChannelIDs(
	registration providers.ChannelPollerRegistration,
	targets youtubePollTargets,
	tieredTargets youtubeTieredPollTargets,
	hasTieredTargets bool,
) []string {
	if registration.TargetGroup == providers.ChannelTargetGroupStats {
		return append([]string(nil), targets.StatsChannelIDs...)
	}
	if registration.TargetGroup == providers.ChannelTargetGroupGlobal {
		return append([]string(nil), registration.ChannelIDs...)
	}
	if isTieredNotificationTargetGroup(registration.TargetGroup) {
		return tieredRegistrationChannelIDs(registration, tieredTargets, hasTieredTargets)
	}
	if registration.TargetGroup == providers.ChannelTargetGroupNotification {
		return append([]string(nil), targets.NotificationChannelIDs...)
	}
	return append([]string(nil), registration.ChannelIDs...)
}

func tieredRegistrationChannelIDs(
	registration providers.ChannelPollerRegistration,
	targets youtubeTieredPollTargets,
	hasTargets bool,
) []string {
	if !hasTargets {
		return append([]string(nil), registration.ChannelIDs...)
	}
	return append([]string(nil), channelIDsForTierGroup(registration.TargetGroup, targets)...)
}

func channelIDsForTierGroup(group providers.ChannelTargetGroup, targets youtubeTieredPollTargets) []string {
	if group == providers.ChannelTargetGroupActive {
		return targets.ActiveNotificationChannelIDs
	}
	if group == providers.ChannelTargetGroupWarm {
		return targets.WarmNotificationChannelIDs
	}
	if group == providers.ChannelTargetGroupCold {
		return targets.ColdNotificationChannelIDs
	}
	return nil
}

func hasTieredNotificationRegistration(registrations []providers.ChannelPollerRegistration) bool {
	for _, registration := range registrations {
		if isTieredNotificationTargetGroup(registration.TargetGroup) {
			return true
		}
	}
	return false
}

func isNotificationTargetGroup(group providers.ChannelTargetGroup) bool {
	return group == providers.ChannelTargetGroupNotification || isTieredNotificationTargetGroup(group)
}

func isTieredNotificationTargetGroup(group providers.ChannelTargetGroup) bool {
	return group == providers.ChannelTargetGroupActive ||
		group == providers.ChannelTargetGroupWarm ||
		group == providers.ChannelTargetGroupCold
}
