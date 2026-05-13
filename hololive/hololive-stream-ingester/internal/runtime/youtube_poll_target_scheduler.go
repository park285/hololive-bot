package runtime

import (
	providers "github.com/kapu/hololive-shared/pkg/providers"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type youTubePollSchedulerSyncer struct {
	scheduler     *poller.Scheduler
	registrations []providers.ChannelPollerRegistration
}

func (s *youTubePollSchedulerSyncer) Sync(targets youtubePollTargets) {
	if s == nil || s.scheduler == nil {
		return
	}
	for _, registration := range s.registrations {
		if !shouldSyncYouTubePollRegistration(registration) {
			continue
		}
		s.scheduler.SyncPollerTargets(youtubePollRegistrationTargetSync(registration, targets))
	}
}

func shouldSyncYouTubePollRegistration(registration providers.ChannelPollerRegistration) bool {
	return registration.Poller != nil &&
		registration.Interval > 0 &&
		registration.HasExplicitChannelIDs
}

func youtubePollRegistrationTargetSync(
	registration providers.ChannelPollerRegistration,
	targets youtubePollTargets,
) poller.PollerTargetSync {
	updated := registration
	updated.ChannelIDs = youtubePollRegistrationChannelIDs(registration, targets)

	sync := updated.ToTargetSync()
	if registration.TargetGroup == providers.ChannelTargetGroupNotification {
		sync.ForceImmediateFirstRun = true
	}
	return sync
}

func youtubePollRegistrationChannelIDs(
	registration providers.ChannelPollerRegistration,
	targets youtubePollTargets,
) []string {
	switch registration.TargetGroup {
	case providers.ChannelTargetGroupStats:
		return append([]string(nil), targets.StatsChannelIDs...)
	case providers.ChannelTargetGroupGlobal:
		return append([]string(nil), registration.ChannelIDs...)
	default:
		return append([]string(nil), targets.NotificationChannelIDs...)
	}
}
