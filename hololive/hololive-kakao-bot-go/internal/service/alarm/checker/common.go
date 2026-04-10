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
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/notification"
)

const (
	defaultLookupConcurrency = 16
)

func uniqueStrings(values []string) []string {
	if len(values) <= 1 {
		return values
	}

	seen := make(map[string]struct{}, len(values))

	unique := make([]string, 0, len(values))
	for _, value := range values {
		if value == "" {
			continue
		}

		if _, ok := seen[value]; ok {
			continue
		}

		seen[value] = struct{}{}
		unique = append(unique, value)
	}

	return unique
}

func cloneStream(stream *domain.Stream) *domain.Stream {
	if stream == nil {
		return nil
	}

	copied := *stream
	if stream.StartScheduled != nil {
		start := *stream.StartScheduled

		copied.StartScheduled = &start
	}

	if stream.StartActual != nil {
		startActual := *stream.StartActual

		copied.StartActual = &startActual
	}

	if stream.Channel != nil {
		channelCopy := *stream.Channel

		copied.Channel = &channelCopy
	}

	return &copied
}

func ensureScheduledTime(stream *domain.Stream, fallback time.Time) *domain.Stream {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		return stream
	}

	updated := cloneStream(stream)
	if updated.StartActual != nil && !updated.StartActual.IsZero() {
		start := updated.StartActual.UTC()

		updated.StartScheduled = &start

		return updated
	}

	fallbackUTC := fallback.UTC().Truncate(time.Minute)

	updated.StartScheduled = &fallbackUTC

	return updated
}

func roomNotifications(
	roomIDs []string,
	channel *domain.Channel,
	stream *domain.Stream,
	minutesUntil int,
	scheduleMessage string,
) []*domain.AlarmNotification {
	if len(roomIDs) == 0 || stream == nil {
		return nil
	}

	notifications := make([]*domain.AlarmNotification, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		if roomID == "" {
			continue
		}

		notifications = append(
			notifications,
			domain.NewAlarmNotification(roomID, channel, stream, minutesUntil, []string{}, scheduleMessage),
		)
	}

	return notifications
}

func loadSubscriberRoomsByChannel(
	ctx context.Context,
	cacheSvc cache.Client,
	channelIDs []string,
) (map[string][]string, error) {
	uniqueChannelIDs := uniqueStrings(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return map[string][]string{}, nil
	}

	result := make(map[string][]string, len(uniqueChannelIDs))

	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(defaultLookupConcurrency)

	for _, channelID := range uniqueChannelIDs {
		eg.Go(func() error {
			rooms, err := cacheSvc.SMembers(egCtx, notification.ChannelSubscribersKeyPrefix+channelID)
			if err != nil {
				return fmt.Errorf("load subscriber rooms by channel: smembers channel %s: %w", channelID, err)
			}

			if len(rooms) == 0 {
				return nil
			}

			mu.Lock()
			result[channelID] = rooms
			mu.Unlock()

			return nil
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("load subscriber rooms by channel: wait workers: %w", err)
	}

	return result, nil
}
func safeLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}
