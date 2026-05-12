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

package outbox

import (
	"context"
	"fmt"
	"log/slog"

	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
)

// outboxItemGroup: Outbox 알림 그룹 (동일 Room + Channel + Kind 묶음)
type outboxItemGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	items     []domain.YouTubeNotificationOutbox
}

type channelAlarmRoomTargets map[domain.AlarmType]map[string]bool

func roomsForItem(roomsByChannel map[string]channelAlarmRoomTargets, item domain.YouTubeNotificationOutbox) (map[string]bool, bool) {
	alarmTargets, ok := roomsByChannel[item.ChannelID]
	if !ok {
		return nil, false
	}

	rooms, ok := alarmTargets[item.Kind.ToAlarmType()]
	if !ok {
		return nil, false
	}

	return rooms, true
}

func (d *Dispatcher) groupOutboxItems(items []domain.YouTubeNotificationOutbox, roomsByChannel map[string]channelAlarmRoomTargets) []*outboxItemGroup {
	if len(items) == 0 {
		return nil
	}

	groups := make([]*outboxItemGroup, 0)
	index := make(map[string]int)

	for i := range items {
		item := &items[i]
		rooms, ok := roomsForItem(roomsByChannel, *item)
		if !ok || len(rooms) == 0 {
			continue
		}

		for roomID := range rooms {
			key := fmt.Sprintf("%s|%s|%s", roomID, item.ChannelID, item.Kind)
			if idx, exists := index[key]; exists {
				groups[idx].items = append(groups[idx].items, *item)
				continue
			}

			groups = append(groups, &outboxItemGroup{
				roomID:    roomID,
				channelID: item.ChannelID,
				kind:      item.Kind,
				items:     []domain.YouTubeNotificationOutbox{*item},
			})
			index[key] = len(groups) - 1
		}
	}

	return groups
}

func (d *Dispatcher) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	result := make(map[string]channelAlarmRoomTargets)

	// 고유 채널 ID + 알람 타입 추출
	type channelEntry struct {
		channelID string
		alarmType domain.AlarmType
	}
	var entries []channelEntry
	seen := make(map[string]bool)
	for i := range items {
		item := &items[i]
		alarmType := item.Kind.ToAlarmType()
		lookupKey := item.ChannelID + "|" + string(alarmType)
		if seen[lookupKey] {
			continue
		}
		seen[lookupKey] = true
		entries = append(entries, channelEntry{channelID: item.ChannelID, alarmType: alarmType})
	}

	if len(entries) == 0 {
		return result
	}

	type lookupResult struct {
		channelID string
		alarmType domain.AlarmType
		rooms     map[string]bool
		ok        bool
	}

	results := make([]lookupResult, len(entries))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.subscriberLookupParallelism())
	for idx := range entries {
		eg.Go(func() error {
			e := entries[idx]
			members, err := sharedalarm.ResolveChannelSubscribersByType(egCtx, d.cache, d.db, e.channelID, e.alarmType)
			if err != nil {
				d.logger.Warn("Failed to get subscribers for channel",
					slog.String("channel_id", e.channelID),
					slog.String("alarm_type", string(e.alarmType)),
					slog.Any("error", err))
				return nil
			}

			roomSet := make(map[string]bool, len(members))
			for _, roomID := range members {
				roomSet[roomID] = true
			}
			results[idx] = lookupResult{
				channelID: e.channelID,
				alarmType: e.alarmType,
				rooms:     roomSet,
				ok:        true,
			}
			return nil
		})
	}
	_ = eg.Wait()

	for i := range results {
		if !results[i].ok {
			continue
		}

		alarmTargets, ok := result[results[i].channelID]
		if !ok {
			alarmTargets = make(channelAlarmRoomTargets)
			result[results[i].channelID] = alarmTargets
		}
		alarmTargets[results[i].alarmType] = results[i].rooms
	}

	return result
}

func (d *Dispatcher) subscriberLookupParallelism() int {
	if d.cfg.SubscriberLookupParallelism <= 0 {
		return DefaultConfig().SubscriberLookupParallelism
	}
	return d.cfg.SubscriberLookupParallelism
}
