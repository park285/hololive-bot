package runtime

import (
	"context"

	providers "github.com/kapu/hololive-shared/pkg/providers"
)

type operationalChannelResolution struct {
	channels     []communityShortsOperationalChannel
	changed      bool
	fallbackUsed bool
}

func (r *youTubePollTargetRefresher) resolveOperationalChannels(ctx context.Context) (operationalChannelResolution, error) {
	if r == nil {
		return operationalChannelResolution{}, nil
	}
	if r.loadOperationalChannels == nil {
		if len(r.lastOperationalChannels) == 0 {
			return operationalChannelResolution{}, nil
		}
		return operationalChannelResolution{
			channels: append([]communityShortsOperationalChannel(nil), r.lastOperationalChannels...),
		}, nil
	}

	operationalChannels, err := r.loadOperationalChannels(ctx)
	if err != nil {
		if len(r.lastOperationalChannels) == 0 {
			return operationalChannelResolution{}, err
		}
		return operationalChannelResolution{
			channels:     append([]communityShortsOperationalChannel(nil), r.lastOperationalChannels...),
			fallbackUsed: true,
		}, nil
	}

	changed := !equalOperationalChannels(r.lastOperationalChannels, operationalChannels)
	r.lastOperationalChannels = append([]communityShortsOperationalChannel(nil), operationalChannels...)
	return operationalChannelResolution{
		channels: append([]communityShortsOperationalChannel(nil), operationalChannels...),
		changed:  changed,
	}, nil
}

func equalOperationalChannels(left, right []communityShortsOperationalChannel) bool {
	if len(left) != len(right) {
		return false
	}
	counts := make(map[communityShortsOperationalChannel]int, len(left))
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

func resolveYouTubePollTargetsFromRegistrations(registrations []providers.ChannelPollerRegistration) youtubePollTargets {
	var notificationChannelIDs []string
	var statsChannelIDs []string

	for _, registration := range registrations {
		notificationChannelIDs, statsChannelIDs = addYouTubePollTargetRegistration(
			notificationChannelIDs,
			statsChannelIDs,
			registration,
		)
	}

	return youtubePollTargets{
		NotificationChannelIDs: notificationChannelIDs,
		StatsChannelIDs:        statsChannelIDs,
	}
}

func addYouTubePollTargetRegistration(
	notificationChannelIDs []string,
	statsChannelIDs []string,
	registration providers.ChannelPollerRegistration,
) ([]string, []string) {
	switch registration.TargetGroup {
	case providers.ChannelTargetGroupStats:
		statsChannelIDs = mergeUniqueChannelIDs(statsChannelIDs, registration.ChannelIDs)
	case providers.ChannelTargetGroupGlobal:
		return notificationChannelIDs, statsChannelIDs
	default:
		notificationChannelIDs = mergeUniqueChannelIDs(notificationChannelIDs, registration.ChannelIDs)
	}
	return notificationChannelIDs, statsChannelIDs
}

func hasYouTubePollTargets(targets youtubePollTargets) bool {
	return len(targets.NotificationChannelIDs) > 0 || len(targets.StatsChannelIDs) > 0
}
