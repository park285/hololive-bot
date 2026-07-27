package polltarget

import (
	"context"
	"log/slog"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/scheduler"
)

const tieringQueryTimeout = 10 * time.Second

type SchedulerSyncer struct {
	scheduler     *scheduler.Scheduler
	registrations []providers.ChannelPollerRegistration
	tieringDB     *pgxpool.Pool
	logger        *slog.Logger
}

func (s *SchedulerSyncer) SyncAt(ctx context.Context, targets Targets, now time.Time) {
	if s == nil || s.scheduler == nil {
		return
	}
	tieredTargets, hasTieredTargets := s.classifyTargetsForTieredRegistrations(ctx, targets, now)
	tieredSyncs := make(map[string][]scheduler.PollerTargetSync)
	for i := range s.registrations {
		registration := &s.registrations[i]
		if !shouldSyncYouTubePollRegistration(registration) {
			continue
		}
		sync := youtubePollRegistrationTargetSync(registration, targets, &tieredTargets, hasTieredTargets)
		if isTieredNotificationTargetGroup(registration.TargetGroup) {
			tieredSyncs[registration.Poller.Name()] = append(tieredSyncs[registration.Poller.Name()], sync)
			continue
		}
		s.scheduler.SyncPollerTargets(&sync)
	}
	for _, syncs := range tieredSyncs {
		s.scheduler.SyncPollerTargetGroups(syncs)
	}
}

func (s *SchedulerSyncer) classifyTargetsForTieredRegistrations(ctx context.Context, targets Targets, now time.Time) (TieredTargets, bool) {
	if !s.hasTieredRegistrations() {
		return TieredTargets{}, false
	}
	if err := ctx.Err(); err != nil {
		s.logTieredClassifySkipped(err)
		return TieredTargets{}, false
	}
	classifyCtx, cancel := context.WithTimeout(ctx, tieringQueryTimeout)
	defer cancel()
	tieredTargets, err := classifyYouTubePollTargetsByActivity(classifyCtx, s.tieringDB, targets, now)
	if err != nil {
		if classifyCtx.Err() != nil {
			s.logTieredClassifySkipped(err)
		}
		return TieredTargets{}, false
	}
	return tieredTargets, true
}

func (s *SchedulerSyncer) logTieredClassifySkipped(err error) {
	if s.logger != nil {
		s.logger.Warn("youtube_poll_target_tiered_classify_skipped", slog.Any("error", err))
	}
}

func (s *SchedulerSyncer) hasTieredRegistrations() bool {
	return s != nil && hasTieredNotificationRegistration(s.registrations)
}

func shouldSyncYouTubePollRegistration(registration *providers.ChannelPollerRegistration) bool {
	return registration != nil &&
		registration.Poller != nil &&
		registration.Interval > 0 &&
		registration.HasExplicitChannelIDs
}

func youtubePollRegistrationTargetSync(
	registration *providers.ChannelPollerRegistration,
	targets Targets,
	tieredTargets *TieredTargets,
	hasTieredTargets bool,
) scheduler.PollerTargetSync {
	if registration == nil {
		return scheduler.PollerTargetSync{}
	}
	updated := cloneYouTubePollRegistration(registration)
	updated.ChannelIDs = youtubePollRegistrationChannelIDs(registration, targets, tieredTargets, hasTieredTargets)

	sync := updated.ToTargetSync()
	if isNotificationTargetGroup(registration.TargetGroup) {
		sync.ForceImmediateFirstRun = true
	}
	return sync
}

func cloneYouTubePollRegistration(registration *providers.ChannelPollerRegistration) providers.ChannelPollerRegistration {
	if registration == nil {
		return providers.ChannelPollerRegistration{}
	}
	clone := *registration
	if registration.ChannelPollerRegistrationOptions != nil {
		options := *registration.ChannelPollerRegistrationOptions
		options.ChannelIDs = append([]string(nil), registration.ChannelIDs...)
		clone.ChannelPollerRegistrationOptions = &options
	}
	return clone
}

func youtubePollRegistrationChannelIDs(
	registration *providers.ChannelPollerRegistration,
	targets Targets,
	tieredTargets *TieredTargets,
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
	registration *providers.ChannelPollerRegistration,
	targets *TieredTargets,
	hasTargets bool,
) []string {
	if !hasTargets || targets == nil {
		return append([]string(nil), registration.ChannelIDs...)
	}
	return append([]string(nil), channelIDsForTierGroup(registration.TargetGroup, targets)...)
}

func channelIDsForTierGroup(group providers.ChannelTargetGroup, targets *TieredTargets) []string {
	if targets == nil {
		return nil
	}
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
	for i := range registrations {
		if isTieredNotificationTargetGroup(registrations[i].TargetGroup) {
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
