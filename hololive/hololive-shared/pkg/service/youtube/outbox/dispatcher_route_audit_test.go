package outbox

import (
	"context"
	"fmt"
	"io"
	"log/slog"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"github.com/glebarez/sqlite"
	"github.com/stretchr/testify/require"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
	cachemocks "github.com/kapu/hololive-shared/pkg/service/cache/mocks"
)

type routeAuditTarget struct {
	channelID string
	alarmType domain.AlarmType
}

type routeAuditCache struct {
	mu           sync.Mutex
	sets         map[string]map[string]struct{}
	hashes       map[string]map[string]string
	lookedUpKeys []string
}

func newRouteAuditCacheClient() (*cachemocks.Client, *routeAuditCache) {
	store := &routeAuditCache{
		sets:   make(map[string]map[string]struct{}),
		hashes: make(map[string]map[string]string),
	}

	client := cachemocks.NewStrictClient()
	client.SAddFunc = func(_ context.Context, key string, members []string) (int64, error) {
		return store.addSetMembers(key, members), nil
	}
	client.SMembersFunc = func(_ context.Context, key string) ([]string, error) {
		return store.members(key), nil
	}
	client.HSetFunc = func(_ context.Context, key, field, value string) error {
		store.setHash(key, field, value)
		return nil
	}
	client.HGetFunc = func(_ context.Context, key, field string) (string, error) {
		return store.getHash(key, field), nil
	}
	client.DelFunc = func(_ context.Context, key string) error {
		store.deleteKey(key)
		return nil
	}

	return client, store
}

func (c *routeAuditCache) addSetMembers(key string, members []string) int64 {
	c.mu.Lock()
	defer c.mu.Unlock()

	set, ok := c.sets[key]
	if !ok {
		set = make(map[string]struct{})
		c.sets[key] = set
	}

	var added int64
	for _, member := range members {
		if _, exists := set[member]; exists {
			continue
		}
		set[member] = struct{}{}
		added++
	}

	return added
}

func (c *routeAuditCache) members(key string) []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	c.lookedUpKeys = append(c.lookedUpKeys, key)

	set := c.sets[key]
	members := make([]string, 0, len(set))
	for member := range set {
		members = append(members, member)
	}
	slices.Sort(members)
	return members
}

func (c *routeAuditCache) setHash(key, field, value string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	hash, ok := c.hashes[key]
	if !ok {
		hash = make(map[string]string)
		c.hashes[key] = hash
	}
	hash[field] = value
}

func (c *routeAuditCache) getHash(key, field string) string {
	c.mu.Lock()
	defer c.mu.Unlock()

	return c.hashes[key][field]
}

func (c *routeAuditCache) deleteKey(key string) {
	c.mu.Lock()
	defer c.mu.Unlock()

	delete(c.sets, key)
	delete(c.hashes, key)
}

func (c *routeAuditCache) lookupKeys() []string {
	c.mu.Lock()
	defer c.mu.Unlock()

	keys := make([]string, len(c.lookedUpKeys))
	copy(keys, c.lookedUpKeys)
	return keys
}

func TestContentAlarmRouteAudit_CoversAllOperationalCommunityShortsTargetsViaTypedKeys(t *testing.T) {
	t.Parallel()

	ctx := context.Background()
	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	require.NoError(t, db.AutoMigrate(&sqliteOutboxModel{}, &sqliteDeliveryModel{}, &sqliteTrackingModel{}, &sqliteTelemetryBufferModel{},
		&domain.YouTubeCommunityShortsAlarmState{},
	))

	cacheSvc, cacheStore := newRouteAuditCacheClient()
	alarms := []*domain.Alarm{
		{RoomID: "room-shorts-a", ChannelID: "UC_A", MemberName: "A", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
		{RoomID: "room-community-a", ChannelID: "UC_A", MemberName: "A", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeCommunity}},
		{RoomID: "room-both-b", ChannelID: "UC_B", MemberName: "B", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts, domain.AlarmTypeCommunity}},
		{RoomID: "room-shorts-b", ChannelID: "UC_B", MemberName: "B", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeShorts}},
		{RoomID: "room-live-only", ChannelID: "UC_LIVE_ONLY", MemberName: "Live", AlarmTypes: domain.AlarmTypes{domain.AlarmTypeLive}},
	}

	summary, err := sharedalarm.WarmSubscriberCacheFromAlarms(ctx, cacheSvc, alarms)
	require.NoError(t, err)
	require.Equal(t, len(alarms), summary.AlarmCount)
	require.Equal(t, 3, summary.ChannelCount)

	expectedTargets := collectRouteAuditTargets(alarms)
	require.Len(t, expectedTargets, 4)

	items := buildRouteAuditOutboxItems(expectedTargets)
	require.NoError(t, db.Create(&items).Error)

	trackingRows := make([]sqliteTrackingModel, 0, len(items))
	for _, item := range items {
		trackingRows = append(trackingRows, sqliteTrackingModel{
			Kind:       string(item.Kind),
			ContentID:  item.ContentID,
			ChannelID:  item.ChannelID,
			DetectedAt: item.NextAttemptAt,
		})
	}
	require.NoError(t, db.Create(&trackingRows).Error)

	sender := &testSender{failRoom: map[string]bool{}}
	dispatcher := NewDispatcher(db, cacheSvc, sender, nil, slog.New(slog.NewTextHandler(io.Discard, nil)), Config{
		BatchSize:           10,
		LockTimeout:         time.Minute,
		PollInterval:        time.Second,
		MaxRetries:          3,
		RetryBackoff:        time.Minute,
		DeliveryParallelism: 1,
	})

	roomsByChannel := dispatcher.collectRoomsByChannel(ctx, items)
	dispatcher.enqueueDeliveries(ctx, items, roomsByChannel)

	var deliveryRows []domain.YouTubeNotificationDelivery
	require.NoError(t, db.Order("id ASC").Find(&deliveryRows).Error)
	require.Len(t, deliveryRows, totalRouteAuditDeliveries(expectedTargets))

	outboxByID := make(map[int64]domain.YouTubeNotificationOutbox, len(items))
	for _, item := range items {
		outboxByID[item.ID] = item
	}

	result := dispatcher.dispatchDeliveryRows(ctx, deliveryRows, outboxByID)
	require.Zerof(t, result.failedDeliveries, "failure buckets: %+v", result.failureBuckets)
	require.Emptyf(t, result.failureBuckets, "failure buckets: %+v", result.failureBuckets)
	require.Len(t, result.successDeliveryIDs, totalRouteAuditDeliveries(expectedTargets))
	require.NoError(t, dispatcher.delivery.MarkSentBatch(ctx, result.successDeliveryIDs))
	require.NoError(t, dispatcher.delivery.UpdateOutboxAggregateStatuses(ctx, result.touchedOutboxIDs))

	require.ElementsMatch(t, expectedRouteAuditLookupKeys(expectedTargets), cacheStore.lookupKeys())
	require.NotContains(t, cacheStore.lookupKeys(), sharedalarmkeys.BuildChannelSubscriberKey("UC_LIVE_ONLY", domain.AlarmTypeLive))

	var deliveries []sqliteDeliveryModel
	require.NoError(t, db.Order("outbox_id ASC, room_id ASC").Find(&deliveries).Error)
	require.Len(t, deliveries, totalRouteAuditDeliveries(expectedTargets))

	gotRoomsByOutbox := make(map[int64][]string, len(items))
	for _, row := range deliveries {
		require.Equal(t, string(domain.OutboxStatusSent), row.Status)
		require.NotNil(t, row.SentAt)
		gotRoomsByOutbox[row.OutboxID] = append(gotRoomsByOutbox[row.OutboxID], row.RoomID)
	}

	for outboxID, wantRooms := range expectedRouteAuditRoomsByOutbox(items, expectedTargets) {
		require.ElementsMatch(t, wantRooms, gotRoomsByOutbox[outboxID])
	}

	var updatedItems []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&updatedItems).Error)
	require.Len(t, updatedItems, len(items))
	for _, item := range updatedItems {
		require.Equal(t, domain.OutboxStatusSent, item.Status)
		require.NotNil(t, item.SentAt)
	}

	require.ElementsMatch(t, expectedRouteAuditSentRooms(expectedTargets), senderRoomIDs(sender.messages))
}

func collectRouteAuditTargets(alarms []*domain.Alarm) map[routeAuditTarget][]string {
	targets := make(map[routeAuditTarget][]string)
	for _, alarm := range alarms {
		if alarm == nil {
			continue
		}

		alarmTypes := alarm.AlarmTypes
		if len(alarmTypes) == 0 {
			alarmTypes = domain.DefaultAlarmTypes
		}

		for _, alarmType := range alarmTypes {
			if alarmType != domain.AlarmTypeCommunity && alarmType != domain.AlarmTypeShorts {
				continue
			}
			target := routeAuditTarget{channelID: alarm.ChannelID, alarmType: alarmType}
			targets[target] = append(targets[target], alarm.RegistryKey())
		}
	}

	for target, rooms := range targets {
		targets[target] = uniqueStrings(rooms)
	}

	return targets
}

func buildRouteAuditOutboxItems(targets map[routeAuditTarget][]string) []domain.YouTubeNotificationOutbox {
	sortedTargets := sortedRouteAuditTargets(targets)
	items := make([]domain.YouTubeNotificationOutbox, 0, len(sortedTargets))
	now := time.Now()

	for _, target := range sortedTargets {
		contentID := fmt.Sprintf("%s-%s", strings.ToLower(string(target.alarmType)), target.channelID)
		item := domain.YouTubeNotificationOutbox{
			Kind:          routeAuditOutboxKind(target.alarmType),
			ChannelID:     target.channelID,
			ContentID:     contentID,
			Status:        domain.OutboxStatusPending,
			AttemptCount:  0,
			NextAttemptAt: now,
		}

		switch target.alarmType {
		case domain.AlarmTypeShorts:
			item.Payload = fmt.Sprintf(`{"video_id":"%s","title":"short-%s"}`, contentID, target.channelID)
		case domain.AlarmTypeCommunity:
			item.Payload = fmt.Sprintf(`{"post_id":"%s","content_text":"community-%s"}`, contentID, target.channelID)
		}

		items = append(items, item)
	}

	return items
}

func routeAuditOutboxKind(alarmType domain.AlarmType) domain.OutboxKind {
	switch alarmType {
	case domain.AlarmTypeShorts:
		return domain.OutboxKindNewShort
	case domain.AlarmTypeCommunity:
		return domain.OutboxKindCommunityPost
	default:
		panic("unexpected route audit alarm type: " + string(alarmType))
	}
}

func sortedRouteAuditTargets(targets map[routeAuditTarget][]string) []routeAuditTarget {
	sortedTargets := make([]routeAuditTarget, 0, len(targets))
	for target := range targets {
		sortedTargets = append(sortedTargets, target)
	}
	slices.SortFunc(sortedTargets, func(left, right routeAuditTarget) int {
		if left.channelID != right.channelID {
			return strings.Compare(left.channelID, right.channelID)
		}
		return strings.Compare(string(left.alarmType), string(right.alarmType))
	})
	return sortedTargets
}

func expectedRouteAuditLookupKeys(targets map[routeAuditTarget][]string) []string {
	sortedTargets := sortedRouteAuditTargets(targets)
	keys := make([]string, 0, len(sortedTargets))
	for _, target := range sortedTargets {
		keys = append(keys, sharedalarmkeys.BuildChannelSubscriberKey(target.channelID, target.alarmType))
	}
	return keys
}

func expectedRouteAuditRoomsByOutbox(items []domain.YouTubeNotificationOutbox, targets map[routeAuditTarget][]string) map[int64][]string {
	roomsByOutbox := make(map[int64][]string, len(items))
	for _, item := range items {
		target := routeAuditTarget{channelID: item.ChannelID, alarmType: item.Kind.ToAlarmType()}
		rooms := append([]string(nil), targets[target]...)
		slices.Sort(rooms)
		roomsByOutbox[item.ID] = rooms
	}
	return roomsByOutbox
}

func expectedRouteAuditSentRooms(targets map[routeAuditTarget][]string) []string {
	sortedTargets := sortedRouteAuditTargets(targets)
	rooms := make([]string, 0, totalRouteAuditDeliveries(targets))
	for _, target := range sortedTargets {
		rooms = append(rooms, targets[target]...)
	}
	slices.Sort(rooms)
	return rooms
}

func totalRouteAuditDeliveries(targets map[routeAuditTarget][]string) int {
	total := 0
	for _, rooms := range targets {
		total += len(rooms)
	}
	return total
}

func senderRoomIDs(messages []string) []string {
	roomIDs := make([]string, 0, len(messages))
	for _, message := range messages {
		roomID, _, found := strings.Cut(message, ":")
		if !found {
			roomIDs = append(roomIDs, message)
			continue
		}
		roomIDs = append(roomIDs, roomID)
	}
	slices.Sort(roomIDs)
	return roomIDs
}
