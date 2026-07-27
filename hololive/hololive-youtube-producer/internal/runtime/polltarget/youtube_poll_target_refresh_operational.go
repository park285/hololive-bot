package polltarget

import (
	"context"

	providers "github.com/kapu/hololive-shared/pkg/providers"
	communityshorts "github.com/kapu/hololive-youtube-producer/internal/communityshorts"
)

type operationalChannelResolution struct {
	channels     []communityshorts.OperationalChannel
	changed      bool
	fallbackUsed bool
}

func (r *Refresher) resolveOperationalChannels(ctx context.Context) (operationalChannelResolution, error) {
	if r == nil {
		return operationalChannelResolution{}, nil
	}
	if r.loadOperationalChannels == nil {
		if len(r.lastOperationalChannels) == 0 {
			return operationalChannelResolution{}, nil
		}
		return operationalChannelResolution{
			channels: append([]communityshorts.OperationalChannel(nil), r.lastOperationalChannels...),
		}, nil
	}

	operationalChannels, err := r.loadOperationalChannels(ctx)
	if err != nil {
		if len(r.lastOperationalChannels) == 0 {
			return operationalChannelResolution{}, err
		}
		return operationalChannelResolution{
			channels:     append([]communityshorts.OperationalChannel(nil), r.lastOperationalChannels...),
			fallbackUsed: true,
		}, nil
	}

	changed := !equalOperationalChannels(r.lastOperationalChannels, operationalChannels)
	r.lastOperationalChannels = append([]communityshorts.OperationalChannel(nil), operationalChannels...)
	return operationalChannelResolution{
		channels: append([]communityshorts.OperationalChannel(nil), operationalChannels...),
		changed:  changed,
	}, nil
}

func equalOperationalChannels(left, right []communityshorts.OperationalChannel) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[communityshorts.OperationalChannel]int, len(left))
	for _, channel := range left {
		counts[channel]++
	}
	for _, channel := range right {
		counts[channel]--
		if counts[channel] < 0 {
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

func resolveYouTubePollTargetsFromRegistrations(registrations []providers.ChannelPollerRegistration) Targets {
	var notificationChannelIDs []string
	var statsChannelIDs []string

	for i := range registrations {
		notificationChannelIDs, statsChannelIDs = addYouTubePollTargetRegistration(
			notificationChannelIDs,
			statsChannelIDs,
			&registrations[i],
		)
	}

	return Targets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
	}
}

func addYouTubePollTargetRegistration(
	notificationChannelIDs []string,
	statsChannelIDs []string,
	registration *providers.ChannelPollerRegistration,
) (resolvedNotificationChannelIDs, resolvedStatsChannelIDs []string) {
	switch registration.TargetGroup {
	case providers.ChannelTargetGroupStats:
		statsChannelIDs = mergeUniqueChannelIDs(statsChannelIDs, registration.ChannelIDs)
	case providers.ChannelTargetGroupGlobal:
		return notificationChannelIDs, statsChannelIDs
	case providers.ChannelTargetGroupDefault,
		providers.ChannelTargetGroupNotification,
		providers.ChannelTargetGroupActive,
		providers.ChannelTargetGroupWarm,
		providers.ChannelTargetGroupCold:
		notificationChannelIDs = mergeUniqueChannelIDs(notificationChannelIDs, registration.ChannelIDs)
	}
	return notificationChannelIDs, statsChannelIDs
}

func hasYouTubePollTargets(targets Targets) bool {
	return len(targets.NotificationChannelIDs) > 0 || len(targets.StatsChannelIDs) > 0
}
