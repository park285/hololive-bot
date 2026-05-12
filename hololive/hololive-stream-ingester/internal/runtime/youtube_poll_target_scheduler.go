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
		s.scheduler.SyncPollerTargets(sync)
	}
}
