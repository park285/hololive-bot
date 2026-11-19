package checking

import (
	"context"
	"errors"
	"fmt"
	"slices"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type upcomingSubscriptionKey struct{ channel, title string }

func withoutUpcomingNotifications(notifications []*domain.AlarmNotification) []*domain.AlarmNotification {
	result := notifications[:0]
	for _, n := range notifications {
		if n == nil || n.Stream == nil || !n.Stream.IsUpcoming() {
			result = append(result, n)
		}
	}

	return result
}

func (c *YouTubeChecker) recoverUpcomingCandidates(ctx context.Context, streams map[string][]*domain.Stream, now time.Time) ([]*domain.AlarmNotification, error) {
	pending, err := c.upcomingCandidates.Pending(ctx, now)
	if err != nil {
		return nil, fmt.Errorf("load pending: %w", err)
	}

	result := make([]*domain.AlarmNotification, 0, len(pending))
	subscriptions := make(map[upcomingSubscriptionKey][]string)
	finished := make(map[string][]string)

	for i := range pending {
		candidate := &pending[i]
		notification := candidate.Notification
		reason := upcomingCandidateTermination(candidate, streams[candidate.ChannelID], now)

		if reason == "" {
			reason, err = c.upcomingSubscriptionTermination(ctx, candidate, subscriptions)
			if err != nil {
				return nil, errors.Join(err, c.finishUpcomingCandidates(ctx, finished, now))
			}
		}

		if reason != "" {
			finished[reason] = append(finished[reason], candidate.DedupeKey)

			continue
		}

		result = append(result, &notification)
	}

	if err := c.finishUpcomingCandidates(ctx, finished, now); err != nil {
		return nil, err
	}

	return result, nil
}

func (c *YouTubeChecker) finishUpcomingCandidates(ctx context.Context, finished map[string][]string, now time.Time) error {
	if len(finished) == 0 {
		return nil
	}

	if err := c.upcomingCandidates.Finish(ctx, finished, now); err != nil {
		return fmt.Errorf("terminate pending: %w", err)
	}

	return nil
}

func (c *YouTubeChecker) upcomingSubscriptionTermination(ctx context.Context, candidate *dispatchoutbox.UpcomingCandidate, subscriptions map[upcomingSubscriptionKey][]string) (string, error) {
	notification := &candidate.Notification
	if notification.Stream == nil {
		return "expired", nil
	}

	key := upcomingSubscriptionKey{candidate.ChannelID, notification.Stream.Title}
	rooms, ok := subscriptions[key]

	if !ok {
		var err error

		rooms, err = c.pendingSubscribers.ResolveEventSubscribers(ctx, key.channel, key.title, domain.AlarmTypeLive)
		if err != nil {
			return "", fmt.Errorf("verify pending subscriptions: %w", err)
		}

		subscriptions[key] = rooms
	}

	if !slices.Contains(rooms, notification.RoomID) {
		return "subscription_removed", nil
	}

	return "", nil
}

func upcomingCandidateTermination(candidate *dispatchoutbox.UpcomingCandidate, streams []*domain.Stream, now time.Time) string {
	snapshot := candidate.Notification.Stream
	if snapshot == nil || snapshot.StartScheduled == nil || !snapshot.StartScheduled.After(now) {
		return "expired"
	}

	for _, current := range streams {
		if current == nil || current.ID != snapshot.ID {
			continue
		}

		if current.IsLive() || current.IsPast() {
			return "stream_ended"
		}

		if current.StartScheduled != nil && !current.StartScheduled.Equal(*snapshot.StartScheduled) {
			return "schedule_changed"
		}
	}

	return ""
}

func cloneCurrentProviderStreams(streams map[string][]*domain.Stream) map[string][]*domain.Stream {
	result := make(map[string][]*domain.Stream, len(streams))
	for channel, items := range streams {
		copied := make([]*domain.Stream, 0, len(items))
		for _, item := range items {
			copied = append(copied, CloneStream(item))
		}

		result[channel] = copied
	}

	return result
}
