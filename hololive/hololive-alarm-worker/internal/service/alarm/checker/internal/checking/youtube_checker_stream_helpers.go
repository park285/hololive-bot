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

package checking

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
)

func (c *YouTubeChecker) buildChannelNotifications(
	ctx context.Context,
	channelID string,
	subscriberRooms []string,
	streams []*domain.Stream,
	window sharedchecker.EvaluationWindow,
	now time.Time,
	liveObservedAtByStreamID ...map[string]time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(streams)*len(subscriberRooms))
	for _, stream := range streams {
		if stream == nil {
			continue
		}

		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, stream, subscriberRooms, window)
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build upcoming notifications: %w", err)
		}

		notifications = append(notifications, upcomingNotifications...)

		liveCatchupNotifications, err := c.buildLiveCatchupNotifications(ctx, channelID, stream, subscriberRooms, now, liveObservedAt(stream, liveObservedAtByStreamID...))
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build live catchup notifications: %w", err)
		}

		notifications = append(notifications, liveCatchupNotifications...)
	}

	return notifications, nil
}

func appendYouTubeChannelNotifications(
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
	channelNotifications []*domain.AlarmNotification,
) {
	if len(channelNotifications) == 0 {
		return
	}

	mu.Lock()
	*notifications = append(*notifications, channelNotifications...)
	mu.Unlock()
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
