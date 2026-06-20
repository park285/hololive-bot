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

package dedup

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

type ScheduleChange struct {
	PreviousScheduled time.Time
	CurrentScheduled  time.Time
	Message           string
}

func (c *ScheduleChange) PreviousScheduledString() string {
	if c == nil || c.PreviousScheduled.IsZero() {
		return ""
	}
	return keys.FormatScheduled(c.PreviousScheduled)
}

func (s *Service) DetectScheduleChange(ctx context.Context, streamID string, currentScheduled time.Time) (string, error) {
	change, err := s.detectStreamScheduleChange(ctx, streamID, currentScheduled)
	if err != nil || change == nil {
		return "", err
	}
	return change.Message, nil
}

func (s *Service) DetectNotificationScheduleChange(ctx context.Context, roomID, channelID string, stream *domain.Stream) (*ScheduleChange, error) {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil, nil
	}

	if change, err := s.detectStreamScheduleChange(ctx, stream.ID, *stream.StartScheduled); err != nil {
		return nil, err
	} else if change != nil {
		return change, nil
	}

	return s.detectLogicalScheduleChange(ctx, roomID, channelID, stream)
}

func (s *Service) TryClaimNotificationScheduleChange(ctx context.Context, roomID, channelID string, stream *domain.Stream, previousScheduled string) (result0 []string, ok1 bool, err error) {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil, false, nil
	}

	oldScheduled, ok, err := s.resolvePreviousNotificationSchedule(ctx, roomID, channelID, stream, previousScheduled)
	if err != nil {
		return nil, false, err
	}
	if !ok {
		return nil, false, nil
	}

	newScheduled := keys.NormalizeScheduledMinute(stream.StartScheduled.UTC())
	if oldScheduled.Equal(newScheduled) {
		return nil, false, nil
	}

	return s.tryClaimNotificationScheduleTransitions(ctx, roomID, channelID, stream, oldScheduled, newScheduled)
}

func (s *Service) resolvePreviousNotificationSchedule(ctx context.Context, roomID, channelID string, stream *domain.Stream, previousScheduled string) (time.Time, bool, error) {
	if oldScheduled, ok := parseScheduledString(previousScheduled); ok {
		return oldScheduled, true, nil
	}

	change, err := s.DetectNotificationScheduleChange(ctx, roomID, channelID, stream)
	if err != nil {
		return time.Time{}, false, err
	}
	if change == nil {
		return time.Time{}, false, nil
	}

	return change.PreviousScheduled, true, nil
}

func (s *Service) tryClaimNotificationScheduleTransitions(ctx context.Context, roomID, channelID string, stream *domain.Stream, oldScheduled, newScheduled time.Time) (result0 []string, ok1 bool, err error) {
	claimKeys := make([]string, 0, 2)
	streamKey, streamClaimed, err := s.TryClaimRoomScheduleTransition(ctx, roomID, stream.ID, oldScheduled, newScheduled)
	if err != nil {
		return claimKeys, false, fmt.Errorf("try claim notification schedule change: stream transition: %w", err)
	}
	if !streamClaimed {
		return claimKeys, false, nil
	}
	claimKeys = append(claimKeys, streamKey)

	logicalKey, logicalClaimed, err := s.TryClaimLogicalScheduleTransition(ctx, roomID, channelID, stream, oldScheduled, newScheduled)
	if err != nil {
		return claimKeys, false, fmt.Errorf("try claim notification schedule change: logical transition: %w", err)
	}
	if !logicalClaimed {
		return claimKeys, false, nil
	}
	claimKeys = append(claimKeys, logicalKey)

	return claimKeys, true, nil
}

func (s *Service) MarkLogicalScheduleObserved(ctx context.Context, roomID, channelID string, stream *domain.Stream) error {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil
	}

	key := keys.BuildLogicalScheduleIndexKey(roomID, channelID, stream.ID, stream.Title)
	data := LogicalScheduleNotifiedData{
		StreamID:       strings.TrimSpace(stream.ID),
		StartScheduled: keys.FormatScheduled(*stream.StartScheduled),
		NotifiedAt:     time.Now().UTC().Format(time.RFC3339),
	}
	if err := s.cache.Set(ctx, key, data, constants.CacheTTL.NotificationSent); err != nil {
		return fmt.Errorf("mark logical schedule observed: set cache key: %w", err)
	}
	return nil
}

func (s *Service) detectStreamScheduleChange(ctx context.Context, streamID string, currentScheduled time.Time) (*ScheduleChange, error) {
	if strings.TrimSpace(streamID) == "" || currentScheduled.IsZero() {
		return nil, nil
	}

	data, err := s.readNotifiedData(ctx, keys.NotifiedKey(streamID))
	if err != nil {
		return nil, fmt.Errorf("detect stream schedule change: read notified data: %w", err)
	}
	if data == nil {
		return nil, nil
	}

	return newScheduleChange(data.StartScheduled, currentScheduled), nil
}

func (s *Service) detectLogicalScheduleChange(ctx context.Context, roomID, channelID string, stream *domain.Stream) (*ScheduleChange, error) {
	if stream == nil || stream.StartScheduled == nil || stream.StartScheduled.IsZero() {
		return nil, nil
	}

	if strings.TrimSpace(roomID) == "" || strings.TrimSpace(channelID) == "" {
		return nil, nil
	}

	indexKey := keys.BuildLogicalScheduleIndexKey(roomID, channelID, stream.ID, stream.Title)

	var data LogicalScheduleNotifiedData
	if err := s.cache.Get(ctx, indexKey, &data); err != nil {
		return nil, fmt.Errorf("detect logical schedule change: get schedule index: %w", err)
	}
	if change := newScheduleChange(data.StartScheduled, *stream.StartScheduled); change != nil {
		return change, nil
	}

	return nil, nil
}

func newScheduleChange(previousScheduled string, currentScheduled time.Time) *ScheduleChange {
	oldScheduled, ok := parseScheduledString(previousScheduled)
	if !ok || currentScheduled.IsZero() {
		return nil
	}

	newScheduled := keys.NormalizeScheduledMinute(currentScheduled.UTC())
	if oldScheduled.Equal(newScheduled) {
		return nil
	}

	message := sharedchecker.FormatScheduleChangeMessage(oldScheduled, newScheduled)
	if message == "" {
		return nil
	}

	return &ScheduleChange{
		PreviousScheduled: oldScheduled,
		CurrentScheduled:  newScheduled,
		Message:           message,
	}
}

func parseScheduledString(raw string) (time.Time, bool) {
	trimmed := strings.TrimSpace(raw)
	if trimmed == "" {
		return time.Time{}, false
	}

	parsed, err := time.Parse(time.RFC3339Nano, trimmed)
	if err != nil {
		return time.Time{}, false
	}

	return keys.NormalizeScheduledMinute(parsed.UTC()), true
}
