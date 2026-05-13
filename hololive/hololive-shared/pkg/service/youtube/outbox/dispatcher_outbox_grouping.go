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

type channelAlarmEntry struct {
	channelID string
	alarmType domain.AlarmType
}

type subscriberLookupResult struct {
	channelID string
	alarmType domain.AlarmType
	rooms     map[string]bool
	ok        bool
}

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

func outboxItemGroupKey(roomID string, item domain.YouTubeNotificationOutbox) string {
	return fmt.Sprintf("%s|%s|%s", roomID, item.ChannelID, item.Kind)
}

func appendOutboxItemGroup(groups []*outboxItemGroup, index map[string]int, roomID string, item domain.YouTubeNotificationOutbox) []*outboxItemGroup {
	key := outboxItemGroupKey(roomID, item)
	if idx, exists := index[key]; exists {
		groups[idx].items = append(groups[idx].items, item)
		return groups
	}

	groups = append(groups, &outboxItemGroup{
		roomID:    roomID,
		channelID: item.ChannelID,
		kind:      item.Kind,
		items:     []domain.YouTubeNotificationOutbox{item},
	})
	index[key] = len(groups) - 1
	return groups
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
			groups = appendOutboxItemGroup(groups, index, roomID, *item)
		}
	}

	return groups
}

func channelAlarmEntriesForItems(items []domain.YouTubeNotificationOutbox) []channelAlarmEntry {
	entries := make([]channelAlarmEntry, 0)
	seen := make(map[string]bool)
	for i := range items {
		item := &items[i]
		alarmType := item.Kind.ToAlarmType()
		lookupKey := item.ChannelID + "|" + string(alarmType)
		if seen[lookupKey] {
			continue
		}
		seen[lookupKey] = true
		entries = append(entries, channelAlarmEntry{channelID: item.ChannelID, alarmType: alarmType})
	}

	return entries
}

func (d *Dispatcher) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	result := make(map[string]channelAlarmRoomTargets)
	entries := channelAlarmEntriesForItems(items)
	if len(entries) == 0 {
		return result
	}

	mergeSubscriberLookupResults(result, d.lookupSubscriberRooms(ctx, entries))
	return result
}

func (d *Dispatcher) lookupSubscriberRooms(ctx context.Context, entries []channelAlarmEntry) []subscriberLookupResult {
	results := make([]subscriberLookupResult, len(entries))
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(d.subscriberLookupParallelism())
	for idx := range entries {
		eg.Go(func() error {
			e := entries[idx]
			rooms, ok := d.resolveSubscriberRooms(egCtx, e)
			results[idx] = subscriberLookupResult{
				channelID: e.channelID,
				alarmType: e.alarmType,
				rooms:     rooms,
				ok:        ok,
			}
			return nil
		})
	}
	_ = eg.Wait()
	return results
}

func (d *Dispatcher) resolveSubscriberRooms(ctx context.Context, entry channelAlarmEntry) (map[string]bool, bool) {
	members, err := sharedalarm.ResolveChannelSubscribersByType(ctx, d.cache, d.db, entry.channelID, entry.alarmType)
	if err != nil {
		d.logger.Warn("Failed to get subscribers for channel",
			slog.String("channel_id", entry.channelID),
			slog.String("alarm_type", string(entry.alarmType)),
			slog.Any("error", err))
		return nil, false
	}

	roomSet := make(map[string]bool, len(members))
	for _, roomID := range members {
		roomSet[roomID] = true
	}
	return roomSet, true
}

func mergeSubscriberLookupResults(result map[string]channelAlarmRoomTargets, results []subscriberLookupResult) {
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
}

func (d *Dispatcher) subscriberLookupParallelism() int {
	if d.cfg.SubscriberLookupParallelism <= 0 {
		return DefaultConfig().SubscriberLookupParallelism
	}
	return d.cfg.SubscriberLookupParallelism
}
