package runtime

import (
	"context"
	"time"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"gorm.io/gorm"
)

type youTubePollSchedulerSyncer struct {
	scheduler     *poller.Scheduler
	registrations []providers.ChannelPollerRegistration
	tieringDB     *gorm.DB
}

func (s *youTubePollSchedulerSyncer) Sync(targets youtubePollTargets) {
	if s == nil || s.scheduler == nil {
		return
	}
	tieredTargets, hasTieredTargets := s.classifyTargetsForTieredRegistrations(targets)
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

func (s *youTubePollSchedulerSyncer) classifyTargetsForTieredRegistrations(targets youtubePollTargets) (youtubeTieredPollTargets, bool) {
	if !hasTieredNotificationRegistration(s.registrations) {
		return youtubeTieredPollTargets{}, false
	}
	tieredTargets, err := classifyYouTubePollTargetsByActivity(context.Background(), s.tieringDB, targets, time.Now())
	if err != nil {
		return youtubeTieredPollTargets{}, false
	}
	return tieredTargets, true
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
