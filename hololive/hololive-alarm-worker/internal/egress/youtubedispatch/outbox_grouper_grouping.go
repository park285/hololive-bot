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

package youtubedispatch

import (
	"context"
	"fmt"
	"log/slog"

	"github.com/park285/shared-go/v2/pkg/panicguard"
	"golang.org/x/sync/errgroup"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// outboxItemGroup: Outbox 알림 그룹 (동일 Room + Channel + Kind 묶음).
type outboxItemGroup struct {
	roomID    string
	channelID string
	kind      domain.OutboxKind
	items     []domain.YouTubeNotificationOutbox
}

type channelAlarmRoomTargets map[domain.AlarmType]map[string]bool

type channelAlarmEntry struct {
	channelID string
	targetKey string
	title     string
	alarmType domain.AlarmType
}

type subscriberLookupResult struct {
	targetKey string
	alarmType domain.AlarmType
	rooms     map[string]bool
	ok        bool
}

func roomsForItem(roomsByChannel map[string]channelAlarmRoomTargets, item *domain.YouTubeNotificationOutbox) (map[string]bool, bool) {
	targetKey, _, err := outboxSubscriberTarget(item)
	if err != nil {
		return nil, false
	}

	alarmTargets, ok := roomsByChannel[targetKey]
	if !ok {
		return nil, false
	}

	rooms, ok := alarmTargets[item.Kind.ToAlarmType()]
	if !ok {
		return nil, false
	}

	return rooms, true
}

func outboxItemGroupKey(roomID string, item *domain.YouTubeNotificationOutbox) string {
	return fmt.Sprintf("%s|%s|%s", roomID, item.ChannelID, item.Kind)
}

func appendOutboxItemGroup(groups []*outboxItemGroup, index map[string]int, roomID string, item *domain.YouTubeNotificationOutbox) []*outboxItemGroup {
	key := outboxItemGroupKey(roomID, item)
	if idx, exists := index[key]; exists {
		groups[idx].items = append(groups[idx].items, *item)
		return groups
	}

	groups = append(groups, &outboxItemGroup{
		roomID:    roomID,
		channelID: item.ChannelID,
		kind:      item.Kind,
		items:     []domain.YouTubeNotificationOutbox{*item},
	})
	index[key] = len(groups) - 1

	return groups
}

func (g *OutboxGrouper) groupOutboxItems(items []domain.YouTubeNotificationOutbox, roomsByChannel map[string]channelAlarmRoomTargets) []*outboxItemGroup {
	if len(items) == 0 {
		return nil
	}

	groups := make([]*outboxItemGroup, 0)
	index := make(map[string]int)

	for i := range items {
		item := &items[i]
		rooms, ok := roomsForItem(roomsByChannel, item)

		if !ok || len(rooms) == 0 {
			continue
		}

		for roomID := range rooms {
			groups = appendOutboxItemGroup(groups, index, roomID, item)
		}
	}

	return groups
}

func (g *OutboxGrouper) channelAlarmEntriesForItems(items []domain.YouTubeNotificationOutbox) []channelAlarmEntry {
	entries := make([]channelAlarmEntry, 0)
	seen := make(map[string]bool)

	for i := range items {
		item := &items[i]

		targetKey, title, err := outboxSubscriberTarget(item)
		if err != nil {
			g.logger.Warn("Failed to read member subscription title", slog.Int64("outbox_id", item.ID), slog.Any("error", err))

			continue
		}

		alarmType := item.Kind.ToAlarmType()
		lookupKey := targetKey + "|" + string(alarmType)

		if seen[lookupKey] {
			continue
		}

		seen[lookupKey] = true

		entries = append(entries, channelAlarmEntry{channelID: item.ChannelID, targetKey: targetKey, title: title, alarmType: alarmType})
	}

	return entries
}

func (g *OutboxGrouper) collectRoomsByChannel(ctx context.Context, items []domain.YouTubeNotificationOutbox) map[string]channelAlarmRoomTargets {
	result := make(map[string]channelAlarmRoomTargets)
	entries := g.channelAlarmEntriesForItems(items)

	if len(entries) == 0 {
		return result
	}

	mergeSubscriberLookupResults(result, g.lookupSubscriberRooms(ctx, entries))

	return result
}

// subscriberLookupGroup은 같은 채널·알림 종류의 조회 항목을 묶는다. UNIT B 채널은 진행자 조합마다 항목이 생기지만
// 구독 목록은 채널·종류 단위로 같으므로 한 번만 읽는다.
type subscriberLookupGroup struct {
	channelID string
	alarmType domain.AlarmType
	entries   []int
}

func groupSubscriberLookups(entries []channelAlarmEntry) []subscriberLookupGroup {
	groups := make([]subscriberLookupGroup, 0, len(entries))
	index := make(map[string]int, len(entries))

	for i := range entries {
		key := entries[i].channelID + "|" + string(entries[i].alarmType)

		position, ok := index[key]
		if !ok {
			position = len(groups)
			index[key] = position

			groups = append(groups, subscriberLookupGroup{channelID: entries[i].channelID, alarmType: entries[i].alarmType})
		}

		groups[position].entries = append(groups[position].entries, i)
	}

	return groups
}

func (g *OutboxGrouper) lookupSubscriberRooms(ctx context.Context, entries []channelAlarmEntry) []subscriberLookupResult {
	results := make([]subscriberLookupResult, len(entries))
	groups := groupSubscriberLookups(entries)
	eg, egCtx := errgroup.WithContext(ctx)
	eg.SetLimit(g.config.SubscriberLookupParallelism)

	for idx := range groups {
		eg.Go(func() error {
			return panicguard.RunE(g.logger, panicguard.BackgroundTask, "youtube-outbox-subscriber-lookup", func() error {
				g.resolveSubscriberGroup(egCtx, entries, groups[idx], results)

				return nil
			})
		})
	}

	if err := eg.Wait(); err != nil {
		g.logger.Warn("Subscriber room lookup worker failed", slog.Any("error", err))
	}

	return results
}

// resolveSubscriberGroup은 묶음의 조회 결과를 각 항목 위치에 기록한다. 조회가 실패하면 묶음의 모든 항목을 실패로
// 남겨, 항목마다 따로 조회하던 때와 같이 해당 대상의 발송을 만들지 않는다.
func (g *OutboxGrouper) resolveSubscriberGroup(ctx context.Context, entries []channelAlarmEntry, group subscriberLookupGroup, results []subscriberLookupResult) {
	titles := make([]string, 0, len(group.entries))
	for _, entryIndex := range group.entries {
		titles = append(titles, entries[entryIndex].title)
	}

	membersByTitle, err := g.lookupSubscribers(ctx, group.channelID, titles, group.alarmType)
	if err != nil {
		g.logger.Warn("Failed to get subscribers for channel",
			slog.String("channel_id", group.channelID),
			slog.String("alarm_type", string(group.alarmType)),
			slog.Any("error", err))
	}

	for _, entryIndex := range group.entries {
		entry := entries[entryIndex]

		results[entryIndex] = subscriberLookupResult{targetKey: entry.targetKey, alarmType: entry.alarmType}

		if err != nil {
			continue
		}

		members := membersByTitle[entry.title]
		roomSet := make(map[string]bool, len(members))

		for _, roomID := range members {
			roomSet[roomID] = true
		}

		results[entryIndex].rooms = roomSet
		results[entryIndex].ok = true
	}
}

func mergeSubscriberLookupResults(result map[string]channelAlarmRoomTargets, results []subscriberLookupResult) {
	for i := range results {
		if !results[i].ok {
			continue
		}

		alarmTargets, ok := result[results[i].targetKey]
		if !ok {
			alarmTargets = make(channelAlarmRoomTargets)
			result[results[i].targetKey] = alarmTargets
		}

		alarmTargets[results[i].alarmType] = results[i].rooms
	}
}
