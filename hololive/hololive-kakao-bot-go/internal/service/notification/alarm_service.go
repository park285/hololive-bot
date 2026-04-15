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

package notification

import (
	"context"
	stdErrors "errors"
	"fmt"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	sharedchecker "github.com/kapu/hololive-shared/pkg/service/alarm/checker"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

const (
	alarmServiceCloseTimeout = 3 * time.Second
	alarmPersistTaskTimeout  = alarmServiceCloseTimeout
)

// alarmServiceCloseOnce: 생성된 AlarmService 인스턴스 레지스트리 (CloseAllAlarmServices 용).
var alarmServiceCloseOnce sync.Map // map[*AlarmService]struct{}

var (
	_ domain.AlarmCRUD          = (*AlarmService)(nil)
	_ domain.AlarmDispatchState = (*AlarmService)(nil)
)

func NewAlarmService(
	cacheSvc cache.Client,
	holodexSvc *holodex.Service,
	chzzkClient *chzzk.Client,
	twitchClient *twitch.Client,
	memberData domain.MemberDataProvider,
	alarmRepo *alarm.Repository,
	logger *slog.Logger,
	advanceMinutes []int,
) (*AlarmService, error) {
	if logger == nil {
		logger = slog.Default()
	}

	initAlarmMetrics()

	targetPolicy := sharedchecker.NewTargetMinutePolicy(sharedchecker.NormalizeTargetMinutes(advanceMinutes))

	var writer alarmWriter

	if alarmRepo != nil {
		writer = alarmRepo
	}

	svc := &AlarmService{
		cache:        cacheSvc,
		holodex:      holodexSvc,
		chzzk:        chzzkClient,
		twitch:       twitchClient,
		memberData:   memberData,
		alarmRepo:    alarmRepo,
		alarmWriter:  writer,
		logger:       logger,
		targetPolicy: targetPolicy,
	}

	alarmServiceCloseOnce.Store(svc, struct{}{})

	return svc, nil
}

func (as *AlarmService) getTargetMinutes() []int {
	as.targetMinutesMu.RLock()
	defer as.targetMinutesMu.RUnlock()

	return as.targetPolicy.Clone()
}

func (as *AlarmService) GetTargetMinutes() []int {
	return as.getTargetMinutes()
}

func (as *AlarmService) UpdateAlarmAdvanceMinutes(_ context.Context, alarmAdvanceMinutes int) []int {
	normalized := sharedchecker.NewTargetMinutePolicyFromRuntimeAdvance(alarmAdvanceMinutes)

	as.targetMinutesMu.Lock()
	as.targetPolicy = normalized
	as.targetMinutesMu.Unlock()

	if as.logger != nil {
		as.logger.Info("Alarm advance minutes updated",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.Any("target_minutes", normalized.Clone()),
		)
	}

	return normalized.Clone()
}

// Close gracefully shuts down the AlarmService, releasing the persist executor.
func (as *AlarmService) Close(_ context.Context) error {
	if as == nil {
		return nil
	}

	return nil
}

// CloseAllAlarmServices closes all AlarmService instances created via NewAlarmService.
func CloseAllAlarmServices(ctx context.Context) error {
	var joinedErr error

	alarmServiceCloseOnce.Range(func(key, _ any) bool {
		svc, ok := key.(*AlarmService)
		if !ok || svc == nil {
			return true
		}

		if err := svc.Close(ctx); err != nil {
			joinedErr = stdErrors.Join(joinedErr, err)
		}

		alarmServiceCloseOnce.Delete(svc)

		return true
	})

	return joinedErr
}

// 방 기반 시스템: room_id가 PRIMARY 키, user_id는 감사(audit) 목적으로 DB에만 기록.
func (as *AlarmService) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	roomID := req.RoomID
	channelID := req.ChannelID

	alarmTypes := normalizedAlarmTypes(req.AlarmTypes)

	alarmKey := as.getAlarmKey(roomID)

	alreadyRegistered, err := as.cache.SIsMember(ctx, alarmKey, channelID)
	if err != nil {
		opErr = fmt.Errorf("check existing alarm: %w", err)
		as.logger.Error("Failed to check existing alarm", slog.Any("error", err))

		return false, opErr
	}

	if alreadyRegistered {
		return false, nil
	}

	record := buildAlarmRecord(req, alarmTypes)

	persistErr := as.persistAlarm(ctx, record)
	if persistErr != nil {
		opErr = fmt.Errorf("persist alarm before cache write: %w", persistErr)
		as.logger.Error("Failed to persist alarm before cache write", slog.Any("error", persistErr))

		return false, opErr
	}

	added, err := as.cacheAlarm(ctx, record)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "add", fmt.Errorf("add alarm: %w", err))
		as.logger.Error("Failed to add alarm", slog.Any("error", opErr))

		return false, fmt.Errorf("rebuild add cache from repository: %w", opErr)
	}

	as.logAlarmAdded(req, alarmTypes)

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil {
		if as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after add",
				slog.Any("error", syncErr),
				slog.String("channel_id", channelID),
				slog.String("room_id", roomID),
			)
		}
	}

	return added > 0, nil
}

func (as *AlarmService) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("remove", startedAt, opErr)
	}()

	exists, err := as.roomAlarmExists(ctx, roomID, channelID)
	if err != nil {
		opErr = err
		return false, opErr
	}

	if !exists {
		return false, nil
	}

	alarmTypes = normalizedRemovalAlarmTypes(alarmTypes)

	deleteErr := as.deleteAlarm(ctx, roomID, channelID)
	if deleteErr != nil {
		opErr = fmt.Errorf("delete alarm before cache removal: %w", deleteErr)
		as.logger.Error("Failed to persist alarm removal before cache write", slog.Any("error", deleteErr))

		return false, opErr
	}

	removed, err := as.removeAlarmFromCache(ctx, roomID, channelID, alarmTypes)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "remove", fmt.Errorf("remove alarm: %w", err))
		as.logger.Error("Failed to remove alarm", slog.Any("error", opErr))

		return false, fmt.Errorf("rebuild remove cache from repository: %w", opErr)
	}

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil {
		if as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after remove",
				slog.Any("error", syncErr),
				slog.String("channel_id", channelID),
				slog.String("room_id", roomID),
			)
		}
	}

	as.logger.Info("Alarm removed",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.Any("alarm_types", alarmTypes),
	)

	if as.alarmRepo != nil {
		return true, nil
	}

	return removed, nil
}

func (as *AlarmService) cacheAlarm(ctx context.Context, record *domain.Alarm) (int64, error) {
	if record == nil {
		return 0, stdErrors.New("alarm is nil")
	}

	alarmKey := as.getAlarmKey(record.RoomID)

	added, err := as.cache.SAdd(ctx, alarmKey, []string{record.ChannelID})
	if err != nil {
		return 0, fmt.Errorf("add room alarm: %w", err)
	}

	registryKey := as.getRegistryKey(record.RoomID)
	if _, err := as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		return 0, fmt.Errorf("add room registry: %w", err)
	}

	builder := as.cache.Builder()

	saddCmds := make([]valkey.Completed, len(record.AlarmTypes))
	for i, alarmType := range record.AlarmTypes {
		subsKey := as.channelSubscribersKeyByType(record.ChannelID, alarmType)

		saddCmds[i] = builder.Sadd().Key(subsKey).Member(registryKey).Build()
	}

	results := as.cache.DoMulti(ctx, saddCmds...)
	if len(results) != len(saddCmds) {
		return 0, fmt.Errorf("add channel subscribers: unexpected result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return 0, fmt.Errorf("add channel subscriber type %s: %w", record.AlarmTypes[i], err)
		}
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{record.ChannelID}); err != nil {
		return 0, fmt.Errorf("add channel registry: %w", err)
	}

	if err := as.CacheMemberName(ctx, record.ChannelID, record.MemberName); err != nil {
		return 0, fmt.Errorf("cache member name: %w", err)
	}

	if record.RoomName != "" {
		if err := as.cache.HSet(ctx, RoomNamesCacheKey, record.RoomID, record.RoomName); err != nil {
			return 0, fmt.Errorf("cache room name: %w", err)
		}
	}

	if record.UserName != "" && record.UserID != "" {
		if err := as.cache.HSet(ctx, UserNamesCacheKey, record.UserID, record.UserName); err != nil {
			return 0, fmt.Errorf("cache user name: %w", err)
		}
	}

	return added, nil
}

func (as *AlarmService) roomAlarmExists(ctx context.Context, roomID, channelID string) (bool, error) {
	if as.alarmRepo != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepo, roomID)
		if err != nil {
			return false, fmt.Errorf("find room alarms: %w", err)
		}

		for _, alarm := range alarms {
			if alarm != nil && alarm.ChannelID == channelID {
				return true, nil
			}
		}

		return false, nil
	}

	exists, err := as.cache.SIsMember(ctx, as.getAlarmKey(roomID), channelID)
	if err != nil {
		return false, fmt.Errorf("check room alarm membership: %w", err)
	}

	return exists, nil
}

func (as *AlarmService) loadRoomAlarmsForMutation(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepo != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepo, roomID)
		if err != nil {
			return nil, fmt.Errorf("find room alarms: %w", err)
		}

		return alarms, nil
	}

	channelIDs, err := as.GetRoomAlarms(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room alarms: %w", err)
	}

	alarms := make([]*domain.Alarm, 0, len(channelIDs))
	for _, channelID := range channelIDs {
		alarms = append(alarms, &domain.Alarm{RoomID: roomID, ChannelID: channelID})
	}

	return alarms, nil
}

func uniqueAlarmChannelIDs(alarms []*domain.Alarm) []string {
	if len(alarms) == 0 {
		return nil
	}

	seen := make(map[string]struct{}, len(alarms))

	channelIDs := make([]string, 0, len(alarms))
	for _, alarm := range alarms {
		if alarm == nil || alarm.ChannelID == "" {
			continue
		}

		if _, ok := seen[alarm.ChannelID]; ok {
			continue
		}

		seen[alarm.ChannelID] = struct{}{}
		channelIDs = append(channelIDs, alarm.ChannelID)
	}

	return channelIDs
}

func (as *AlarmService) removeAlarmFromCache(
	ctx context.Context,
	roomID, channelID string,
	alarmTypes domain.AlarmTypes,
) (bool, error) {
	alarmKey := as.getAlarmKey(roomID)

	removed, err := as.cache.SRem(ctx, alarmKey, []string{channelID})
	if err != nil {
		return false, fmt.Errorf("remove room alarm: %w", err)
	}

	registryKey := as.getRegistryKey(roomID)

	removeSubscribersErr := as.removeChannelSubscribers(ctx, channelID, registryKey, alarmTypes)
	if removeSubscribersErr != nil {
		return false, fmt.Errorf("remove channel subscribers: %w", removeSubscribersErr)
	}

	cleanupRegistryErr := as.cleanupChannelRegistryIfEmpty(ctx, channelID)
	if cleanupRegistryErr != nil {
		return false, fmt.Errorf("cleanup channel registry if empty: %w", cleanupRegistryErr)
	}

	remainingAlarms, err := as.cache.SMembers(ctx, alarmKey)
	if err != nil {
		return false, fmt.Errorf("read remaining room alarms: %w", err)
	}

	if len(remainingAlarms) == 0 {
		if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
			return false, fmt.Errorf("remove room registry: %w", err)
		}

		as.logger.Info("Room removed from registry (no alarms left)",
			slog.String("room_id", roomID),
		)
	}

	return removed > 0, nil
}

func (as *AlarmService) clearRoomAlarmsFromCache(ctx context.Context, roomID string, channelIDs []string) (int, error) {
	if len(channelIDs) == 0 {
		return 0, nil
	}

	alarmKey := as.getAlarmKey(roomID)

	removed, err := as.cache.SRem(ctx, alarmKey, channelIDs)
	if err != nil {
		return 0, fmt.Errorf("remove room alarms: %w", err)
	}

	registryKey := as.getRegistryKey(roomID)
	if err := as.clearChannelSubscribersPipeline(ctx, channelIDs, registryKey); err != nil {
		return 0, fmt.Errorf("clear channel subscribers pipeline: %w", err)
	}

	if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		return 0, fmt.Errorf("remove room registry: %w", err)
	}

	return int(removed), nil
}

func (as *AlarmService) removeChannelSubscribers(
	ctx context.Context,
	channelID, registryKey string,
	alarmTypes domain.AlarmTypes,
) error {
	if len(alarmTypes) == 0 {
		return nil
	}

	builder := as.cache.Builder()
	subscriberKeys := as.channelSubscriberKeys(channelID, alarmTypes)

	if err := as.executeSubscriberTypeRemoval(ctx, builder, subscriberKeys, registryKey, alarmTypes); err != nil {
		return fmt.Errorf("execute subscriber type removal: %w", err)
	}

	cleanupKeys, err := as.collectEmptySubscriberKeys(ctx, builder, subscriberKeys, alarmTypes, "remove channel subscribers")
	if err != nil {
		return fmt.Errorf("collect empty subscriber keys: %w", err)
	}

	if err := as.deleteSubscriberKeys(ctx, builder, cleanupKeys, "remove channel subscribers"); err != nil {
		return fmt.Errorf("delete subscriber keys: %w", err)
	}

	return nil
}

func (as *AlarmService) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarmKey := as.getAlarmKey(roomID)

	channelIDs, err := as.cache.SMembers(ctx, alarmKey)
	if err != nil {
		as.logger.Error("Failed to get room alarms", slog.Any("error", err))
		return []string{}, fmt.Errorf("get room alarms: %w", err)
	}

	return channelIDs, nil
}

func (as *AlarmService) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepo == nil {
		return nil, stdErrors.New("alarm repository not configured")
	}

	alarms, err := as.alarmRepo.FindByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	return alarms, nil
}

func (as *AlarmService) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	startedAt := time.Now()

	var opErr error

	defer func() {
		observeAlarmServiceOperation("clear", startedAt, opErr)
	}()

	alarmRecords, err := as.loadRoomAlarmsForMutation(ctx, roomID)
	if err != nil {
		opErr = err

		return 0, fmt.Errorf("load room alarms for mutation: %w", err)
	}

	if len(alarmRecords) == 0 {
		return 0, nil
	}

	deleteErr := as.deleteRoomAlarms(ctx, roomID)
	if deleteErr != nil {
		opErr = fmt.Errorf("delete room alarms before cache clear: %w", deleteErr)
		as.logger.Error("Failed to persist room alarm clear before cache write", slog.Any("error", deleteErr))

		return 0, opErr
	}

	channelIDs := uniqueAlarmChannelIDs(alarmRecords)

	removed, err := as.clearRoomAlarmsFromCache(ctx, roomID, channelIDs)
	if err != nil {
		opErr = as.rebuildAlarmCacheFromRepository(ctx, "clear", fmt.Errorf("clear room alarms: %w", err))
		as.logger.Error("Failed to clear room alarms", slog.Any("error", opErr))

		return 0, fmt.Errorf("rebuild clear cache from repository: %w", opErr)
	}

	for _, channelID := range channelIDs {
		if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil && as.logger != nil {
			as.logger.Warn("Failed to cleanup channel registry during room alarm clear",
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
				slog.Any("error", err),
			)
		}

		if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after clear",
				slog.Any("error", syncErr),
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
			)
		}
	}

	as.logger.Info("All alarms cleared",
		slog.String("room_id", roomID),
		slog.Int("count", len(channelIDs)),
	)

	if as.alarmRepo != nil {
		return len(channelIDs), nil
	}

	return removed, nil
}

func (as *AlarmService) clearChannelSubscribersPipeline(ctx context.Context, alarms []string, registryKey string) error {
	if len(alarms) == 0 {
		return nil
	}

	builder := as.cache.Builder()
	channelSubsKeys := as.roomChannelSubscriberKeys(alarms)

	if err := as.executeSubscriberKeyRemoval(ctx, builder, channelSubsKeys, registryKey, "clear channel subscribers"); err != nil {
		return fmt.Errorf("execute subscriber key removal: %w", err)
	}

	cleanupKeys, err := as.collectEmptySubscriberKeys(ctx, builder, channelSubsKeys, nil, "clear channel subscribers")
	if err != nil {
		return fmt.Errorf("collect empty subscriber keys: %w", err)
	}

	if err := as.deleteSubscriberKeys(ctx, builder, cleanupKeys, "clear channel subscribers"); err != nil {
		return fmt.Errorf("delete subscriber keys: %w", err)
	}

	return nil
}

func normalizedAlarmTypes(alarmTypes domain.AlarmTypes) domain.AlarmTypes {
	if len(alarmTypes) == 0 {
		return domain.DefaultAlarmTypes
	}

	return alarmTypes
}

func normalizedRemovalAlarmTypes(alarmTypes domain.AlarmTypes) domain.AlarmTypes {
	if len(alarmTypes) == 0 {
		return domain.AllAlarmTypes
	}

	return alarmTypes
}

func buildAlarmRecord(req domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) *domain.Alarm {
	return &domain.Alarm{
		RoomID:     req.RoomID,
		UserID:     req.UserID,
		ChannelID:  req.ChannelID,
		MemberName: req.MemberName,
		RoomName:   req.RoomName,
		UserName:   req.UserName,
		AlarmTypes: alarmTypes,
	}
}

func (as *AlarmService) logAlarmAdded(req domain.AddAlarmRequest, alarmTypes domain.AlarmTypes) {
	as.logger.Info("Alarm added",
		slog.String("room_id", req.RoomID),
		slog.String("room_name", req.RoomName),
		slog.String("user_id", req.UserID),
		slog.String("user_name", req.UserName),
		slog.String("channel_id", req.ChannelID),
		slog.String("member_name", req.MemberName),
		slog.Any("alarm_types", alarmTypes),
	)
}

func (as *AlarmService) channelSubscriberKeys(channelID string, alarmTypes domain.AlarmTypes) []string {
	keys := make([]string, len(alarmTypes))
	for i, alarmType := range alarmTypes {
		keys[i] = as.channelSubscribersKeyByType(channelID, alarmType)
	}

	return keys
}

func (as *AlarmService) roomChannelSubscriberKeys(channelIDs []string) []string {
	keys := make([]string, 0, len(channelIDs)*len(domain.AllAlarmTypes))
	for _, channelID := range channelIDs {
		keys = append(keys, as.channelSubscriberKeys(channelID, domain.AllAlarmTypes)...)
	}

	return keys
}

func (as *AlarmService) executeSubscriberTypeRemoval(
	ctx context.Context,
	builder valkey.Builder,
	subscriberKeys []string,
	registryKey string,
	alarmTypes domain.AlarmTypes,
) error {
	results := as.cache.DoMulti(ctx, buildSubscriberSRemCommands(builder, subscriberKeys, registryKey)...)
	if len(results) != len(subscriberKeys) {
		return fmt.Errorf("remove channel subscribers: unexpected SREM result count: %d", len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("remove channel subscribers: srem type %s: %w", alarmTypes[i], err)
		}
	}

	return nil
}

func (as *AlarmService) executeSubscriberKeyRemoval(
	ctx context.Context,
	builder valkey.Builder,
	subscriberKeys []string,
	registryKey string,
	operation string,
) error {
	results := as.cache.DoMulti(ctx, buildSubscriberSRemCommands(builder, subscriberKeys, registryKey)...)
	if len(results) != len(subscriberKeys) {
		return fmt.Errorf("%s: unexpected SREM result count: %d", operation, len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("%s: srem key %s: %w", operation, subscriberKeys[i], err)
		}
	}

	return nil
}

func buildSubscriberSRemCommands(builder valkey.Builder, subscriberKeys []string, registryKey string) []valkey.Completed {
	commands := make([]valkey.Completed, len(subscriberKeys))
	for i, key := range subscriberKeys {
		commands[i] = builder.Srem().Key(key).Member(registryKey).Build()
	}

	return commands
}

func (as *AlarmService) collectEmptySubscriberKeys(
	ctx context.Context,
	builder valkey.Builder,
	subscriberKeys []string,
	alarmTypes domain.AlarmTypes,
	operation string,
) ([]string, error) {
	scardCommands := make([]valkey.Completed, len(subscriberKeys))
	for i, key := range subscriberKeys {
		scardCommands[i] = builder.Scard().Key(key).Build()
	}

	results := as.cache.DoMulti(ctx, scardCommands...)
	if len(results) != len(scardCommands) {
		return nil, fmt.Errorf("%s: unexpected SCARD result count: %d", operation, len(results))
	}

	cleanupKeys := make([]string, 0, len(results))
	for i, result := range results {
		count, err := result.AsInt64()
		if err != nil {
			if len(alarmTypes) > 0 {
				return nil, fmt.Errorf("%s: scard type %s: %w", operation, alarmTypes[i], err)
			}

			return nil, fmt.Errorf("%s: scard key %s: %w", operation, subscriberKeys[i], err)
		}

		if count == 0 {
			cleanupKeys = append(cleanupKeys, subscriberKeys[i])
		}
	}

	return cleanupKeys, nil
}

func (as *AlarmService) deleteSubscriberKeys(
	ctx context.Context,
	builder valkey.Builder,
	cleanupKeys []string,
	operation string,
) error {
	if len(cleanupKeys) == 0 {
		return nil
	}

	cleanupCommands := make([]valkey.Completed, len(cleanupKeys))
	for i, key := range cleanupKeys {
		cleanupCommands[i] = builder.Del().Key(key).Build()
	}

	results := as.cache.DoMulti(ctx, cleanupCommands...)
	if len(results) != len(cleanupCommands) {
		return fmt.Errorf("%s: unexpected DEL result count: %d", operation, len(results))
	}

	for i, result := range results {
		if err := result.Error(); err != nil {
			return fmt.Errorf("%s: delete key %s: %w", operation, cleanupKeys[i], err)
		}
	}

	return nil
}

func (as *AlarmService) cleanupChannelRegistryIfEmpty(ctx context.Context, channelID string) error {
	builder := as.cache.Builder()

	allSubsKeys := make([]string, 0, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		allSubsKeys = append(allSubsKeys, as.channelSubscribersKeyByType(channelID, alarmType))
	}

	scardCmds := make([]valkey.Completed, 0, len(allSubsKeys))
	for _, k := range allSubsKeys {
		scardCmds = append(scardCmds, builder.Scard().Key(k).Build())
	}

	scardResults := as.cache.DoMulti(ctx, scardCmds...)
	if len(scardResults) != len(scardCmds) {
		return fmt.Errorf("cleanup channel registry: unexpected SCARD result count: %d", len(scardResults))
	}

	for i, res := range scardResults {
		count, err := res.AsInt64()
		if err != nil {
			return fmt.Errorf("cleanup channel registry: scard key %s: %w", allSubsKeys[i], err)
		}

		if count > 0 {
			return nil
		}
	}

	if _, err := as.cache.SRem(ctx, AlarmChannelRegistryKey, []string{channelID}); err != nil {
		return fmt.Errorf("cleanup channel registry: remove channel registry entry: %w", err)
	}

	return nil
}
