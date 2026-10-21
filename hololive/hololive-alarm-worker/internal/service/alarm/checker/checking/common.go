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
	"log/slog"
	"maps"
	"time"

	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/alarmstore"
	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	alarmkeys "github.com/kapu/hololive-alarm-worker/internal/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

func UniqueStrings(values []string) []string {
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

func CloneStream(stream *domain.Stream) *domain.Stream {
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

	copied.CollaboTalentNames = cloneStringSlice(stream.CollaboTalentNames)

	return &copied
}

func cloneStringSlice(values []string) []string {
	if values == nil {
		return nil
	}

	cloned := make([]string, len(values))
	copy(cloned, values)

	return cloned
}

func EnsureScheduledTime(stream *domain.Stream, fallback time.Time) *domain.Stream {
	if stream == nil {
		return nil
	}

	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		return stream
	}

	updated := CloneStream(stream)
	if updated.StartActual != nil && !updated.StartActual.IsZero() {
		start := updated.StartActual.UTC()

		updated.StartScheduled = &start

		return updated
	}

	fallbackUTC := fallback.UTC().Truncate(time.Minute)

	updated.StartScheduled = &fallbackUTC

	return updated
}

func RoomNotifications(
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

func RoomNotificationsWithScheduleChanges(
	roomIDs []string,
	channel *domain.Channel,
	stream *domain.Stream,
	minutesUntil int,
	scheduleChanges map[string]*dedup.ScheduleChange,
	scheduleChangeOnly bool,
) []*domain.AlarmNotification {
	if len(roomIDs) == 0 || stream == nil {
		return nil
	}

	notifications := make([]*domain.AlarmNotification, 0, len(roomIDs))
	for _, roomID := range roomIDs {
		if roomID == "" {
			continue
		}

		change := scheduleChanges[roomID]
		if !ShouldSendScheduleChangeNotification(change, scheduleChangeOnly) {
			continue
		}

		scheduleMessage, previousScheduled := ScheduleChangeNotificationDetails(change)
		notification := domain.NewAlarmNotification(roomID, channel, stream, minutesUntil, []string{}, scheduleMessage)

		notification.ScheduleChangePreviousStart = previousScheduled
		notifications = append(notifications, notification)
	}

	return notifications
}

func ShouldSendScheduleChangeNotification(change *dedup.ScheduleChange, scheduleChangeOnly bool) bool {
	if !scheduleChangeOnly {
		return true
	}

	return change != nil
}

func ScheduleChangeNotificationDetails(change *dedup.ScheduleChange) (message, previousScheduled string) {
	if change == nil {
		return "", ""
	}

	return change.Message, change.PreviousScheduledString()
}

// LoadSubscriberRoomsByChannel은 채널별 LIVE 구독 방을 반환한다. 구독자가 있는 채널만 결과 map에 담는다.
// Cache set이 비어 있는 채널은 subscribers resolver의 empty marker와 구독 DB로 확정한다.
func LoadSubscriberRoomsByChannel(
	ctx context.Context,
	cacheClient cache.Client,
	subscribers *alarmstore.SubscriberResolver,
	channelIDs []string,
) (map[string][]string, error) {
	uniqueChannelIDs := UniqueStrings(channelIDs)
	if len(uniqueChannelIDs) == 0 {
		return map[string][]string{}, nil
	}

	result, err := loadCachedSubscriberRoomsByChannel(ctx, cacheClient, uniqueChannelIDs)
	if err != nil {
		return nil, fmt.Errorf("load subscriber rooms by channel: load cache: %w", err)
	}

	uncachedChannelIDs := make([]string, 0, len(uniqueChannelIDs))
	for _, channelID := range uniqueChannelIDs {
		if len(result[channelID]) == 0 {
			uncachedChannelIDs = append(uncachedChannelIDs, channelID)
		}
	}

	if len(uncachedChannelIDs) == 0 {
		return result, nil
	}

	// TTL 없는 구독 set은 eviction으로 사라질 수 있어 빈 set을 구독 0으로 단정하면 LIVE 알림이 조용히 빠진다.
	// empty marker가 없는 채널은 이번 cycle에만 DB로 확정하고 set은 다시 채우지 않는다(read-through).
	recovered, err := subscribers.ResolveUncachedChannelSubscribersByType(ctx, uncachedChannelIDs, domain.AlarmTypeLive)
	if err != nil {
		return nil, fmt.Errorf("load subscriber rooms by channel: resolve uncached channels: %w", err)
	}

	maps.Copy(result, recovered)

	return result, nil
}

func loadCachedSubscriberRoomsByChannel(
	ctx context.Context,
	cacheClient cache.Client,
	uniqueChannelIDs []string,
) (map[string][]string, error) {
	cmds := make([]valkey.Completed, 0, len(uniqueChannelIDs))
	for _, channelID := range uniqueChannelIDs {
		cmds = append(cmds, cacheClient.B().Smembers().Key(alarmkeys.ChannelSubscribersKeyPrefix+channelID).Build())
	}

	results := cacheClient.DoMulti(ctx, cmds...)
	if len(results) != len(uniqueChannelIDs) {
		return nil, fmt.Errorf("load subscriber rooms by channel: unexpected result count %d for %d channels", len(results), len(uniqueChannelIDs))
	}

	return CollectBatchedSubscriberRooms(results, uniqueChannelIDs)
}

func CollectBatchedSubscriberRooms(
	results []valkey.ValkeyResult,
	uniqueChannelIDs []string,
) (map[string][]string, error) {
	result := make(map[string][]string, len(uniqueChannelIDs))
	for i, channelID := range uniqueChannelIDs {
		rooms, err := results[i].AsStrSlice()
		if err != nil {
			return nil, fmt.Errorf("load subscriber rooms by channel: smembers channel %s: %w", channelID, err)
		}

		if len(rooms) > 0 {
			result[channelID] = rooms
		}
	}

	return result, nil
}

func SafeLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}
