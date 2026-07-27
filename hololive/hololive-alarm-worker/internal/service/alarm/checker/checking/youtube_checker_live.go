package checking

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	sharedconstants "github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedlog "github.com/park285/shared-go/pkg/logging"
)

func (c *YouTubeChecker) buildLiveCatchupNotifications(
	ctx context.Context,
	channelID string,
	stream *domain.Stream,
	subscriberRooms []string,
	now time.Time,
	observedAt ...*time.Time,
) ([]*domain.AlarmNotification, error) {
	if !isLiveCatchupCandidate(stream) {
		return nil, nil
	}

	startAt, ok := resolveEligibleLiveCatchupStart(stream, now, observedAt...)
	if !ok {
		return nil, nil
	}

	minutesUntil := c.targetPolicySnapshot().PrimaryAdvanceMinute()
	resolvedStream := EnsureScheduledTime(stream, *startAt)
	if resolvedStream == nil {
		return nil, nil
	}
	notifications, suppressedRooms, err := c.unsuppressedLiveCatchupNotifications(ctx, channelID, resolvedStream, subscriberRooms, minutesUntil)
	if err != nil {
		return nil, err
	}
	return c.finalizeLiveCatchupNotifications(ctx, stream, startAt, now, notifications, suppressedRooms), nil
}

func isLiveCatchupCandidate(stream *domain.Stream) bool {
	return stream != nil && stream.IsLive()
}

func resolveEligibleLiveCatchupStart(stream *domain.Stream, now time.Time, observedAt ...*time.Time) (*time.Time, bool) {
	startAt := resolveLiveStart(stream)
	if startAt == nil {
		observeYouTubeLiveCatchup("missing_start")
		return nil, false
	}
	if startAt.After(now) {
		observeYouTubeLiveCatchup("future_start")
		return nil, false
	}
	if now.Sub(*startAt) > sharedconstants.LiveCatchupWindow {
		if liveObservedRecently(observedAt, now) {
			return startAt, true
		}
		observeYouTubeLiveCatchup("outside_window")
		return nil, false
	}
	return startAt, true
}

func liveObservedRecently(observedAt []*time.Time, now time.Time) bool {
	if len(observedAt) == 0 || observedAt[0] == nil || observedAt[0].IsZero() {
		return false
	}
	observed := observedAt[0].UTC()
	return !observed.After(now) && now.Sub(observed) <= sharedconstants.LiveCatchupWindow
}

func (c *YouTubeChecker) unsuppressedLiveCatchupNotifications(
	ctx context.Context,
	channelID string,
	stream *domain.Stream,
	subscriberRooms []string,
	minutesUntil int,
) ([]*domain.AlarmNotification, int, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(subscriberRooms))
	suppressedRooms := 0
	for _, roomID := range subscriberRooms {
		recentlyUpcoming, err := c.roomHasRecentUpcomingNotification(ctx, roomID, channelID, stream)
		if err != nil {
			if ctx.Err() != nil {
				return nil, 0, err
			}
			c.logger.Warn("live catchup dedup check failed, skipping room",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.String("error", err.Error()))
			continue
		}
		if recentlyUpcoming {
			suppressedRooms++
			continue
		}
		notifications = append(notifications, RoomNotifications([]string{roomID}, stream.Channel, stream, minutesUntil, "")...)
	}
	return notifications, suppressedRooms, nil
}

func (c *YouTubeChecker) roomHasRecentUpcomingNotification(ctx context.Context, roomID, channelID string, stream *domain.Stream) (bool, error) {
	recentlyUpcoming, err := c.dedupService.WasUpcomingEventNotifiedRecently(
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
	ctx context.Context,
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
		if suppressedRooms > 0 {
			sharedlog.Debug(ctx, c.logger, "alarm.youtube.live_catchup.suppressed", "youtube live catchup alarm suppressed",
				slog.String("stream_id", stream.ID),
				slog.String("channel_id", youtubeStreamChannelID(stream)),
				slog.Time("start_at", startAt.UTC()),
				slog.Int("suppressed_rooms", suppressedRooms),
			)
		}
		return notifications
	}

	observeYouTubeLiveCatchup("selected")
	sharedlog.Debug(ctx, c.logger, "alarm.youtube.live_catchup.selected", "youtube live catchup alarm selected",
		slog.String("stream_id", stream.ID),
		slog.String("channel_id", youtubeStreamChannelID(stream)),
		slog.Time("start_at", startAt.UTC()),
		slog.Int64("elapsed_seconds", int64(now.Sub(*startAt)/time.Second)),
		slog.Int("minutes_until", notifications[0].MinutesUntil),
		slog.Int("rooms", len(notifications)),
		slog.Int("suppressed_rooms", suppressedRooms),
	)
	return notifications
}
