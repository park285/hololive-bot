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

package alarmservice

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/park285/shared-go/pkg/stringutil"
)

func buildTitleFingerprint(title, streamID string) string {
	return keys.BuildTitleFingerprint(title, streamID)
}

func resolveStreamChannelID(stream *domain.Stream, defaultChannelID string) string {
	if stream == nil {
		return defaultChannelID
	}

	channelID := stringutil.TrimSpace(stream.ChannelID)
	if channelID != "" {
		return channelID
	}

	if stream.Channel != nil {
		channelID = stringutil.TrimSpace(stream.Channel.ID)
		if channelID != "" {
			return channelID
		}
	}

	return defaultChannelID
}

func (as *AlarmService) buildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	scheduledMinute := normalizeScheduledMinute(startScheduled).Unix()
	titleFingerprint := buildTitleFingerprint(title, streamID)

	return fmt.Sprintf(
		"%s%s:%s:%d:%s",
		UpcomingEventKeyPrefix,
		roomID,
		channelID,
		scheduledMinute,
		titleFingerprint,
	)
}

func (as *AlarmService) MarkUpcomingEventNotified(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
) error {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	resolvedChannelID := resolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return nil
	}

	key := as.buildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	data := UpcomingEventNotifiedData{
		NotifiedAt: time.Now().UTC().Format(time.RFC3339),
	}
	if err := as.cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		as.logger.Warn("Failed to mark upcoming event notified",
			slog.String("key", key),
			slog.String("room_id", roomID),
			slog.String("channel_id", resolvedChannelID),
			slog.String("stream_id", stream.ID),
			slog.Any("error", err),
		)

		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}

func (as *AlarmService) WasUpcomingEventNotifiedRecently(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
	window time.Duration,
) bool {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return false
	}

	resolvedChannelID := resolveStreamChannelID(stream, channelID)
	if stringutil.TrimSpace(resolvedChannelID) == "" {
		return false
	}

	key := as.buildUpcomingEventKey(roomID, resolvedChannelID, stream.ID, stream.Title, *stream.StartScheduled)

	var data UpcomingEventNotifiedData
	if err := as.cache.Get(ctx, key, &data); err != nil || data.NotifiedAt == "" {
		return false
	}

	notifiedAt, err := time.Parse(time.RFC3339, data.NotifiedAt)
	if err != nil {
		return false
	}

	if window <= 0 {
		return false
	}

	return time.Since(notifiedAt) <= window
}
