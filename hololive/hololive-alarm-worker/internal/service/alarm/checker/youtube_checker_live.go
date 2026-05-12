// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

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
	if stream == nil || !stream.IsLive() {
		return nil, nil
	}

	startAt := resolveLiveStart(stream)
	if startAt == nil {
		observeYouTubeLiveCatchup("missing_start")
		return nil, nil
	}
	if startAt.After(now) {
		observeYouTubeLiveCatchup("future_start")
		return nil, nil
	}

	catchupWindow := sharedconstants.FullRefreshInterval + time.Minute
	if now.Sub(*startAt) > catchupWindow {
		observeYouTubeLiveCatchup("outside_window")
		return nil, nil
	}

	alreadyNotified, err := c.dedupSvc.IsAlreadyNotified(ctx, stream.ID)
	if err != nil {
		return nil, fmt.Errorf("build live catchup notifications: check already notified: %w", err)
	}

	if alreadyNotified {
		observeYouTubeLiveCatchup("already_notified")
		return nil, nil
	}

	resolvedStream := ensureScheduledTime(stream, *startAt)

	notifications := make([]*domain.AlarmNotification, 0, len(subscriberRooms))
	suppressedRooms := 0
	for _, roomID := range subscriberRooms {
		recentlyUpcoming, err := c.dedupSvc.WasUpcomingEventNotifiedRecently(
			ctx,
			roomID,
			channelID,
			resolvedStream,
			sharedconstants.LiveCatchupSuppressWindow,
		)
		if err != nil {
			return nil, fmt.Errorf("build live catchup notifications: check upcoming suppress window: %w", err)
		}

		if recentlyUpcoming {
			suppressedRooms++
			continue
		}

		notifications = append(notifications, roomNotifications([]string{roomID}, resolvedStream.Channel, resolvedStream, 0, "")...)
	}

	if suppressedRooms > 0 {
		observeYouTubeLiveCatchup("suppressed_recent_upcoming")
	}
	if len(notifications) == 0 {
		return notifications, nil
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

	return notifications, nil
}

func resolveLiveStart(stream *domain.Stream) *time.Time {
	if stream == nil {
		return nil
	}

	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		start := stream.StartActual.UTC()
		return &start
	}

	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		start := stream.StartScheduled.UTC()
		return &start
	}

	return nil
}

func groupStreamsByChannel(streams []*domain.Stream) map[string][]*domain.Stream {
	grouped := make(map[string][]*domain.Stream)

	for _, stream := range streams {
		if stream == nil {
			continue
		}

		channelID := stream.ChannelID
		if channelID == "" && stream.Channel != nil {
			channelID = stream.Channel.ID
		}

		if channelID == "" {
			continue
		}

		grouped[channelID] = append(grouped[channelID], stream)
	}

	return grouped
}

func youtubeStreamChannelID(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}
	if stream.ChannelID != "" {
		return stream.ChannelID
	}
	if stream.Channel != nil {
		return stream.Channel.ID
	}
	return ""
}
