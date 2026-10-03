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
	"errors"
	"fmt"
	"log/slog"
	"maps"
	"strings"
	"sync"
	"time"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"github.com/valkey-io/valkey-go"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dedup"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	"github.com/kapu/hololive-shared/pkg/service/cache"
)

const (
	DefaultLookupConcurrency = 16
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

func LoadMemberNamesByChannel(ctx context.Context, cacheClient cache.Client, channelIDs []string) (map[string]string, error) {
	channelIDs = UniqueStrings(channelIDs)
	if len(channelIDs) == 0 {
		return map[string]string{}, nil
	}

	memberNames, err := cacheClient.BatchHGet(ctx, sharedalarmkeys.MemberNameKey, channelIDs)
	if err != nil {
		return nil, fmt.Errorf("load member names by channel: %w", err)
	}

	return memberNames, nil
}

func ApplyMemberNamesToStreams(streamsByChannel map[string][]*domain.Stream, memberNames map[string]string) {
	for channelID, streams := range streamsByChannel {
		memberName := strings.TrimSpace(memberNames[channelID])
		if memberName == "" {
			continue
		}

		for _, stream := range streams {
			ApplyMemberNameToStream(stream, channelID, memberName)
		}
	}
}

func ApplyMemberNameToStream(stream *domain.Stream, channelID, memberName string) {
	if stream == nil {
		return
	}

	stream.ChannelName = memberName
	if stream.Channel == nil {
		stream.Channel = &domain.Channel{ID: channelID}
	}

	if strings.TrimSpace(stream.Channel.ID) == "" {
		stream.Channel.ID = channelID
	}

	stream.Channel.Name = memberName
}

func ChannelNameForMember(channelID, memberName, fallback string) string {
	if memberName = strings.TrimSpace(memberName); memberName != "" {
		return memberName
	}

	if fallback = strings.TrimSpace(fallback); fallback != "" {
		return fallback
	}

	return strings.TrimSpace(channelID)
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

// ErrBatchedSubscriberRoomsUnavailable: DoMulti 파이프라인을 쓸 수 없어 순차 조회로 되돌려야 함을 알리는 sentinel.
// 조회 실패가 아니라 경로 전환 신호이므로 호출자는 이 오류를 밖으로 전파하지 않는다.
var ErrBatchedSubscriberRoomsUnavailable = errors.New("batched subscriber rooms unavailable")

// LoadSubscriberRoomsByChannel은 채널별 LIVE 구독 방을 반환한다. 구독자가 있는 채널만 결과 map에 담는다.
func LoadSubscriberRoomsByChannel(
	ctx context.Context,
	cacheClient cache.Client,
	subscriptionDB dbx.Querier,
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
	recovered, err := sharedalarm.ResolveUncachedChannelSubscribersByType(ctx, cacheClient, subscriptionDB, uncachedChannelIDs, domain.AlarmTypeLive)
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
	result, err := TryLoadSubscriberRoomsByChannelBatched(ctx, cacheClient, uniqueChannelIDs)
	if err == nil {
		return result, nil
	}

	if !errors.Is(err, ErrBatchedSubscriberRoomsUnavailable) {
		return nil, fmt.Errorf("load subscriber rooms by channel batched: %w", err)
	}

	out, sequentialErr := LoadSubscriberRoomsByChannelSequential(ctx, cacheClient, uniqueChannelIDs)
	if sequentialErr != nil {
		return nil, fmt.Errorf("load subscriber rooms by channel sequential: %w", sequentialErr)
	}

	return out, nil
}

func TryLoadSubscriberRoomsByChannelBatched(
	ctx context.Context,
	cacheClient cache.Client,
	uniqueChannelIDs []string,
) (result map[string][]string, err error) {
	defer func() {
		// DoMulti 파이프라인이 panic 하면 순차 조회로 되돌려야 하므로 sentinel 로 바꿔 전파한다.
		if recovered := recover(); recovered != nil {
			result, err = nil, ErrBatchedSubscriberRoomsUnavailable
		}
	}()

	client := cacheClient.GetClient()
	if client == nil {
		return nil, ErrBatchedSubscriberRoomsUnavailable
	}

	cmds := make([]valkey.Completed, 0, len(uniqueChannelIDs))
	for _, channelID := range uniqueChannelIDs {
		cmds = append(cmds, client.B().Smembers().Key(sharedalarmkeys.ChannelSubscribersKeyPrefix+channelID).Build())
	}

	results := cacheClient.DoMulti(ctx, cmds...)
	if len(results) != len(uniqueChannelIDs) {
		return nil, ErrBatchedSubscriberRoomsUnavailable
	}

	collected, collectErr := CollectBatchedSubscriberRooms(results, uniqueChannelIDs)
	if collectErr != nil {
		return nil, fmt.Errorf("collect batched subscriber rooms: %w", collectErr)
	}

	return collected, nil
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

func LoadSubscriberRoomsByChannelSequential(
	ctx context.Context,
	cacheClient cache.Client,
	uniqueChannelIDs []string,
) (map[string][]string, error) {
	result := make(map[string][]string, len(uniqueChannelIDs))

	var mu sync.Mutex

	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(DefaultLookupConcurrency)

	for _, channelID := range uniqueChannelIDs {
		eg.Go(func() error {
			return panicguard.RunE(nil, panicguard.BackgroundTask, "subscriber-room-lookup", func() error {
				rooms, err := cacheClient.SMembers(egCtx, sharedalarmkeys.ChannelSubscribersKeyPrefix+channelID)
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
		})
	}

	if err := eg.Wait(); err != nil {
		return nil, fmt.Errorf("load subscriber rooms by channel: wait workers: %w", err)
	}

	return result, nil
}

func SafeLogger(logger *slog.Logger) *slog.Logger {
	if logger == nil {
		return slog.Default()
	}

	return logger
}
