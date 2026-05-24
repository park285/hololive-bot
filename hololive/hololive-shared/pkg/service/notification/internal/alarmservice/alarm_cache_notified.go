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
	"strings"
	"time"

	"github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/constants"
)

func notifiedMinuteKey(streamID string, startScheduled time.Time, minutesUntil int) string {
	normalizedScheduled := normalizeScheduledMinute(startScheduled).Unix()

	return fmt.Sprintf(
		"%s%s:%d:%d",
		NotifiedKeyPrefix,
		strings.TrimSpace(streamID),
		normalizedScheduled,
		minutesUntil,
	)
}

func (as *AlarmService) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return fmt.Errorf("mark as notified: stream id is empty")
	}

	canonicalKey := notifiedMinuteKey(streamID, startScheduled, minutesUntil)
	if err := as.cache.Set(ctx, canonicalKey, "1", constants.CacheTTL.NotificationSent); err != nil {
		return logging.LogAndWrapError(ctx, as.logger, "mark as notified", err,
			slog.String("stream_id", streamID),
			slog.Int("minutes_until", minutesUntil),
		)
	}

	if err := as.updateLegacyNotifiedData(ctx, streamID, startScheduled, minutesUntil); err != nil {
		return logging.LogAndWrapError(ctx, as.logger, "mark as notified legacy data", err,
			slog.String("stream_id", streamID),
			slog.Int("minutes_until", minutesUntil),
		)
	}

	return nil
}

func (as *AlarmService) updateLegacyNotifiedData(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	as.notifiedLegacyMu.Lock()
	defer as.notifiedLegacyMu.Unlock()

	notifiedKey := NotifiedKeyPrefix + streamID
	scheduledStr := normalizeScheduledMinute(startScheduled).Format(time.RFC3339)

	var existing NotifiedData
	if err := as.cache.Get(ctx, notifiedKey, &existing); err != nil || existing.StartScheduled == "" {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}

	if existing.StartScheduled != scheduledStr {
		existing = NotifiedData{StartScheduled: scheduledStr, SentAt: make(map[int]bool)}
	}
	if existing.SentAt == nil {
		existing.SentAt = make(map[int]bool)
	}

	existing.SentAt[minutesUntil] = true

	if err := as.cache.Set(ctx, notifiedKey, existing, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("set legacy notified data: %w", err)
	}

	return nil
}

func (as *AlarmService) WasNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) bool {
	streamID = strings.TrimSpace(streamID)
	if streamID == "" {
		return false
	}

	var marker string
	if err := as.cache.Get(ctx, notifiedMinuteKey(streamID, startScheduled, minutesUntil), &marker); err == nil && marker == "1" {
		return true
	}

	var legacy NotifiedData
	if err := as.cache.Get(ctx, NotifiedKeyPrefix+streamID, &legacy); err != nil {
		return false
	}
	if legacy.StartScheduled != normalizeScheduledMinute(startScheduled).Format(time.RFC3339) {
		return false
	}

	return legacy.SentAt != nil && legacy.SentAt[minutesUntil]
}
