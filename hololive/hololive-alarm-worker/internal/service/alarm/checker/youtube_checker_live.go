package checker

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *YouTubeChecker) buildLiveCatchupNotifications(
	ctx context.Context,
	channelID string,
	stream *domain.Stream,
	subscriberRooms []string,
	now time.Time,
) ([]*domain.AlarmNotification, error) {
	if !isLiveCatchupCandidate(stream) {
		return nil, nil
	}

	startAt, ok := resolveEligibleLiveCatchupStart(stream, now)
	if !ok {
		return nil, nil
	}

	alreadyNotified, err := c.liveCatchupAlreadyNotified(ctx, stream)
	if err != nil {
		return nil, err
	}
	if alreadyNotified {
		return nil, nil
	}

	resolvedStream := ensureScheduledTime(stream, *startAt)
	notifications, suppressedRooms, err := c.unsuppressedLiveCatchupNotifications(ctx, channelID, resolvedStream, subscriberRooms)
	if err != nil {
		return nil, err
	}
	return c.finalizeLiveCatchupNotifications(stream, startAt, now, notifications, suppressedRooms), nil
}

func isLiveCatchupCandidate(stream *domain.Stream) bool {
	return stream != nil && stream.IsLive()
}

func resolveEligibleLiveCatchupStart(stream *domain.Stream, now time.Time) (*time.Time, bool) {
	startAt := resolveLiveStart(stream)
	if startAt == nil {
		observeYouTubeLiveCatchup("missing_start")
		return nil, false
	}
	if startAt.After(now) {
		observeYouTubeLiveCatchup("future_start")
		return nil, false
	}
	if now.Sub(*startAt) > sharedconstants.FullRefreshInterval+time.Minute {
		observeYouTubeLiveCatchup("outside_window")
		return nil, false
	}
	return startAt, true
}

func (c *YouTubeChecker) liveCatchupAlreadyNotified(ctx context.Context, stream *domain.Stream) (bool, error) {
	alreadyNotified, err := c.dedupSvc.IsAlreadyNotified(ctx, stream.ID)
	if err != nil {
		return false, fmt.Errorf("build live catchup notifications: check already notified: %w", err)
	}
	if alreadyNotified {
		observeYouTubeLiveCatchup("already_notified")
	}
	return alreadyNotified, nil
}

func (c *YouTubeChecker) unsuppressedLiveCatchupNotifications(
	ctx context.Context,
	channelID string,
	stream *domain.Stream,
	subscriberRooms []string,
) ([]*domain.AlarmNotification, int, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(subscriberRooms))
	suppressedRooms := 0
	for _, roomID := range subscriberRooms {
		recentlyUpcoming, err := c.roomHasRecentUpcomingNotification(ctx, roomID, channelID, stream)
		if err != nil {
			return nil, 0, err
		}
		if recentlyUpcoming {
			suppressedRooms++
			continue
		}
		notifications = append(notifications, roomNotifications([]string{roomID}, stream.Channel, stream, 0, "")...)
	}
	return notifications, suppressedRooms, nil
}

func (c *YouTubeChecker) roomHasRecentUpcomingNotification(ctx context.Context, roomID, channelID string, stream *domain.Stream) (bool, error) {
	recentlyUpcoming, err := c.dedupSvc.WasUpcomingEventNotifiedRecently(
		ctx,
		roomID,
		channelID,
		stream,
		sharedconstants.LiveCatchupSuppressWindow,
	)
	if err != nil {
		return false, fmt.Errorf("build live catchup notifications: check upcoming suppress window: %w", err)
	}
	return recentlyUpcoming, nil
}

func (c *YouTubeChecker) finalizeLiveCatchupNotifications(
	stream *domain.Stream,
	startAt *time.Time,
	now time.Time,
	notifications []*domain.AlarmNotification,
	suppressedRooms int,
) []*domain.AlarmNotification {
	if suppressedRooms > 0 {
		observeYouTubeLiveCatchup("suppressed_recent_upcoming")
	}
	if len(notifications) == 0 {
		return notifications
	}

	observeYouTubeLiveCatchup("selected")
	c.logger.Info("YouTube live catchup alarm selected",
		slog.String("stream_id", stream.ID),
		slog.String("channel_id", youtubeStreamChannelID(stream)),
		slog.Time("start_at", startAt.UTC()),
		slog.Int64("elapsed_seconds", int64(now.Sub(*startAt)/time.Second)),
		slog.Int("rooms", len(notifications)),
		slog.Int("suppressed_rooms", suppressedRooms),
	)
	return notifications
}
