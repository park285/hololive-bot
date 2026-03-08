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
	"slices"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/holodex"
	"github.com/park285/llm-kakao-bots/shared-go/pkg/workerpool"
	"github.com/valkey-io/valkey-go"

	"github.com/kapu/hololive-kakao-bot-go/internal/service/chzzk"
	"github.com/kapu/hololive-kakao-bot-go/internal/service/twitch"
)

const (
	alarmServiceCloseTimeout = 3 * time.Second
	alarmPersistTaskTimeout  = alarmServiceCloseTimeout
)

// alarmServiceCloseOnce: 생성된 AlarmService 인스턴스 레지스트리 (CloseAllAlarmServices 용)
var alarmServiceCloseOnce sync.Map // map[*AlarmService]struct{}

var (
	_ domain.AlarmCRUD          = (*AlarmService)(nil)
	_ domain.AlarmDispatchState = (*AlarmService)(nil)
)

// NewAlarmService: 새로운 AlarmService 인스턴스를 생성하고 설정(목표 알림 시간 등)을 초기화합니다.
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

	targetMinutes := buildTargetMinutes(advanceMinutes)

	pool, err := workerpool.New(workerpool.DefaultConfig())
	if err != nil {
		return nil, fmt.Errorf("create alarm persist pool: %w", err)
	}

	svc := &AlarmService{
		cache:         cacheSvc,
		holodex:       holodexSvc,
		chzzk:         chzzkClient,
		twitch:        twitchClient,
		memberData:    memberData,
		alarmRepo:     alarmRepo,
		logger:        logger,
		targetMinutes: targetMinutes,
		persistPool:   pool,
	}

	alarmServiceCloseOnce.Store(svc, struct{}{})

	return svc, nil
}

func buildTargetMinutes(advanceMinutes []int) []int {
	filtered := make([]int, 0, len(advanceMinutes))
	seen := make(map[int]struct{})

	for _, minute := range advanceMinutes {
		if minute <= 0 {
			continue
		}
		if _, exists := seen[minute]; exists {
			continue
		}
		seen[minute] = struct{}{}
		filtered = append(filtered, minute)
	}

	if len(filtered) == 0 {
		return []int{5, 3, 1}
	}

	slices.SortFunc(filtered, func(a, b int) int { return b - a })

	if _, hasFallback := seen[1]; !hasFallback {
		filtered = append(filtered, 1)
	}

	return filtered
}

func buildRuntimeTargetMinutes(alarmAdvanceMinutes int) []int {
	return buildTargetMinutes([]int{alarmAdvanceMinutes, 3, 1})
}

func (as *AlarmService) getTargetMinutes() []int {
	as.targetMinutesMu.RLock()
	defer as.targetMinutesMu.RUnlock()

	if len(as.targetMinutes) == 0 {
		return []int{5, 3, 1}
	}

	targetMinutes := make([]int, len(as.targetMinutes))
	copy(targetMinutes, as.targetMinutes)
	return targetMinutes
}

func (as *AlarmService) GetTargetMinutes() []int {
	return as.getTargetMinutes()
}

func (as *AlarmService) UpdateAlarmAdvanceMinutes(_ context.Context, alarmAdvanceMinutes int) []int {
	normalized := buildRuntimeTargetMinutes(alarmAdvanceMinutes)
	as.targetMinutesMu.Lock()
	as.targetMinutes = normalized
	as.targetMinutesMu.Unlock()

	if as.logger != nil {
		as.logger.Info("Alarm advance minutes updated",
			slog.Int("alarm_advance_minutes", alarmAdvanceMinutes),
			slog.Any("target_minutes", normalized),
		)
	}

	return normalized
}

// Close gracefully shuts down the AlarmService, releasing the worker pool.
func (as *AlarmService) Close(ctx context.Context) error {
	if as == nil {
		return nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	var closeErr error
	as.closeOnce.Do(func() {
		if as.persistPool == nil {
			return
		}

		shutdownCtx, cancel := context.WithTimeout(ctx, alarmServiceCloseTimeout)
		defer cancel()

		if err := as.persistPool.ShutdownWait(shutdownCtx); err != nil {
			closeErr = fmt.Errorf("shutdown alarm persist pool: %w", err)
		}
	})

	return closeErr
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

// AddAlarm: 특정 채팅방에 대해 특정 멤버(채널)의 방송 알림을 추가합니다.
// 방 기반 시스템: room_id가 PRIMARY 키, user_id는 감사(audit) 목적으로 DB에만 기록
func (as *AlarmService) AddAlarm(ctx context.Context, req domain.AddAlarmRequest) (bool, error) {
	startedAt := time.Now()
	var opErr error
	defer func() {
		observeAlarmServiceOperation("add", startedAt, opErr)
	}()

	roomID := req.RoomID
	channelID := req.ChannelID
	memberName := req.MemberName
	roomName := req.RoomName
	userName := req.UserName
	alarmTypes := req.AlarmTypes
	if len(alarmTypes) == 0 {
		alarmTypes = domain.DefaultAlarmTypes
	}

	alarmKey := as.getAlarmKey(roomID)
	added, err := as.cache.SAdd(ctx, alarmKey, []string{channelID})
	if err != nil {
		opErr = fmt.Errorf("add alarm: %w", err)
		as.logger.Error("Failed to add alarm", slog.Any("error", err))
		return false, opErr
	}

	registryKey := as.getRegistryKey(roomID)
	if _, err := as.cache.SAdd(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
		as.logger.Warn("Failed to add to registry", slog.Any("error", err))
	}

	// Pipeline Redis writes for alarm type subscriptions
	client := as.cache.GetClient()
	saddCmds := make([]valkey.Completed, len(alarmTypes))
	for i, alarmType := range alarmTypes {
		subsKey := as.channelSubscribersKeyByType(channelID, alarmType)
		saddCmds[i] = client.B().Sadd().Key(subsKey).Member(registryKey).Build()
	}
	results := as.cache.DoMulti(ctx, saddCmds...)
	for i, result := range results {
		if err := result.Error(); err != nil {
			as.logger.Warn("Failed to add channel subscriber",
				slog.String("type", string(alarmTypes[i])),
				slog.Any("error", err))
		}
	}

	if _, err := as.cache.SAdd(ctx, AlarmChannelRegistryKey, []string{channelID}); err != nil {
		as.logger.Warn("Failed to add to channel registry", slog.Any("error", err))
	}

	if err := as.CacheMemberName(ctx, channelID, memberName); err != nil {
		as.logger.Warn("Failed to cache member name", slog.Any("error", err))
	}

	if roomName != "" {
		_ = as.cache.HSet(ctx, RoomNamesCacheKey, roomID, roomName)
	}
	if userName != "" {
		_ = as.cache.HSet(ctx, UserNamesCacheKey, req.UserID, userName)
	}

	as.persistAlarmAsync(&domain.Alarm{
		RoomID:     roomID,
		UserID:     req.UserID,
		ChannelID:  channelID,
		MemberName: memberName,
		RoomName:   roomName,
		UserName:   userName,
		AlarmTypes: alarmTypes,
	})

	as.logger.Info("Alarm added",
		slog.String("room_id", roomID),
		slog.String("room_name", roomName),
		slog.String("user_id", req.UserID),
		slog.String("user_name", userName),
		slog.String("channel_id", channelID),
		slog.String("member_name", memberName),
		slog.Any("alarm_types", alarmTypes),
	)

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

// RemoveAlarm: 특정 채팅방에서 특정 멤버(채널)의 방송 알림을 해제합니다. (방 기반)
func (as *AlarmService) RemoveAlarm(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) (bool, error) {
	startedAt := time.Now()
	var opErr error
	defer func() {
		observeAlarmServiceOperation("remove", startedAt, opErr)
	}()

	if len(alarmTypes) == 0 {
		alarmTypes = domain.AllAlarmTypes
	}

	alarmKey := as.getAlarmKey(roomID)
	removed, err := as.cache.SRem(ctx, alarmKey, []string{channelID})
	if err != nil {
		opErr = fmt.Errorf("remove alarm: %w", err)
		as.logger.Error("Failed to remove alarm", slog.Any("error", err))
		return false, opErr
	}

	registryKey := as.getRegistryKey(roomID)
	as.removeChannelSubscribers(ctx, channelID, registryKey, alarmTypes)

	as.cleanupChannelRegistryIfEmpty(ctx, channelID)

	remainingAlarms, err := as.cache.SMembers(ctx, alarmKey)
	if err == nil && len(remainingAlarms) == 0 {
		_, _ = as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey})
		as.logger.Info("Room removed from registry (no alarms left)",
			slog.String("room_id", roomID),
		)
	}

	as.removeAlarmAsync(roomID, channelID)

	as.logger.Info("Alarm removed",
		slog.String("room_id", roomID),
		slog.String("channel_id", channelID),
		slog.Any("alarm_types", alarmTypes),
	)

	if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil {
		if as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after remove",
				slog.Any("error", syncErr),
				slog.String("channel_id", channelID),
				slog.String("room_id", roomID),
			)
		}
	}

	return removed > 0, nil
}

func (as *AlarmService) removeChannelSubscribers(
	ctx context.Context,
	channelID, registryKey string,
	alarmTypes domain.AlarmTypes,
) {
	builder := as.cache.Builder()
	sremCmds := make([]valkey.Completed, 0, len(alarmTypes))
	for _, alarmType := range alarmTypes {
		subsKey := as.channelSubscribersKeyByType(channelID, alarmType)
		sremCmds = append(sremCmds, builder.Srem().Key(subsKey).Member(registryKey).Build())
	}

	if len(sremCmds) > 0 {
		results := as.cache.DoMulti(ctx, sremCmds...)
		for i, res := range results {
			if sremErr := res.Error(); sremErr != nil {
				as.logger.Warn("Failed to remove from channel subscribers",
					slog.String("type", string(alarmTypes[i])),
					slog.Any("error", sremErr))
			}
		}
	}

	smembersCmds := make([]valkey.Completed, 0, len(alarmTypes))
	for _, alarmType := range alarmTypes {
		subsKey := as.channelSubscribersKeyByType(channelID, alarmType)
		smembersCmds = append(smembersCmds, builder.Smembers().Key(subsKey).Build())
	}
	if len(smembersCmds) == 0 {
		return
	}

	smembersResults := as.cache.DoMulti(ctx, smembersCmds...)
	cleanupCmds := make([]valkey.Completed, 0, len(smembersResults))
	for i, res := range smembersResults {
		members, memErr := res.AsStrSlice()
		if memErr == nil && len(members) == 0 {
			subsKey := as.channelSubscribersKeyByType(channelID, alarmTypes[i])
			cleanupCmds = append(cleanupCmds, builder.Del().Key(subsKey).Build())
		}
	}

	if len(cleanupCmds) > 0 {
		_ = as.cache.DoMulti(ctx, cleanupCmds...)
	}
}

// GetRoomAlarms: 해당 방이 구독 중인 모든 채널 ID 목록을 반환합니다.
func (as *AlarmService) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarmKey := as.getAlarmKey(roomID)
	channelIDs, err := as.cache.SMembers(ctx, alarmKey)
	if err != nil {
		as.logger.Error("Failed to get room alarms", slog.Any("error", err))
		return []string{}, fmt.Errorf("get room alarms: %w", err)
	}
	return channelIDs, nil
}

// GetRoomAlarmsWithTypes: 해당 방의 알람 목록을 타입 정보와 함께 반환합니다.
func (as *AlarmService) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepo == nil {
		return nil, fmt.Errorf("alarm repository not configured")
	}
	alarms, err := as.alarmRepo.FindByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}
	return alarms, nil
}

// ClearRoomAlarms: 해당 방의 모든 알림 설정을 삭제(초기화)합니다.
func (as *AlarmService) ClearRoomAlarms(ctx context.Context, roomID string) (int, error) {
	startedAt := time.Now()
	var opErr error
	defer func() {
		observeAlarmServiceOperation("clear", startedAt, opErr)
	}()

	alarms, err := as.GetRoomAlarms(ctx, roomID)
	if err != nil {
		opErr = err
		return 0, err
	}

	if len(alarms) == 0 {
		return 0, nil
	}

	alarmKey := as.getAlarmKey(roomID)
	removed, err := as.cache.SRem(ctx, alarmKey, alarms)
	if err != nil {
		opErr = fmt.Errorf("clear room alarms: %w", err)
		as.logger.Error("Failed to clear room alarms", slog.Any("error", err))
		return 0, opErr
	}

	registryKey := as.getRegistryKey(roomID)

	as.clearChannelSubscribersPipeline(ctx, alarms, registryKey)
	for _, channelID := range alarms {
		as.cleanupChannelRegistryIfEmpty(ctx, channelID)
		if syncErr := as.syncPlatformMappingForChannel(ctx, channelID); syncErr != nil && as.logger != nil {
			as.logger.Warn("Failed to sync platform alarm mapping after clear",
				slog.Any("error", syncErr),
				slog.String("room_id", roomID),
				slog.String("channel_id", channelID),
			)
		}
	}

	_, _ = as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey})

	as.clearRoomAlarmsAsync(roomID)

	as.logger.Info("All alarms cleared",
		slog.String("room_id", roomID),
		slog.Int("count", int(removed)),
	)

	return int(removed), nil
}

func (as *AlarmService) clearChannelSubscribersPipeline(ctx context.Context, alarms []string, registryKey string) {
	if len(alarms) == 0 {
		return
	}

	client := as.cache.GetClient()

	channelSubsKeys := make([]string, 0, len(alarms)*len(domain.AllAlarmTypes))
	sremCmds := make([]valkey.Completed, 0, len(alarms)*len(domain.AllAlarmTypes))
	for _, channelID := range alarms {
		for _, alarmType := range domain.AllAlarmTypes {
			key := as.channelSubscribersKeyByType(channelID, alarmType)
			channelSubsKeys = append(channelSubsKeys, key)
			sremCmds = append(sremCmds, client.B().Srem().Key(key).Member(registryKey).Build())
		}
	}
	_ = as.cache.DoMulti(ctx, sremCmds...)

	smembersCmds := make([]valkey.Completed, len(channelSubsKeys))
	for i, key := range channelSubsKeys {
		smembersCmds[i] = client.B().Smembers().Key(key).Build()
	}
	smembersResults := as.cache.DoMulti(ctx, smembersCmds...)

	cleanupCmds := make([]valkey.Completed, 0, len(smembersResults))
	for i, result := range smembersResults {
		members, err := result.AsStrSlice()
		if err != nil || len(members) > 0 {
			continue
		}
		cleanupCmds = append(cleanupCmds, client.B().Del().Key(channelSubsKeys[i]).Build())
	}

	if len(cleanupCmds) > 0 {
		_ = as.cache.DoMulti(ctx, cleanupCmds...)
	}
}

func (as *AlarmService) cleanupChannelRegistryIfEmpty(ctx context.Context, channelID string) {
	allEmpty := true
	builder := as.cache.Builder()
	allSubsKeys := make([]string, 0, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		allSubsKeys = append(allSubsKeys, as.channelSubscribersKeyByType(channelID, alarmType))
	}

	allSmembersCmds := make([]valkey.Completed, 0, len(allSubsKeys))
	for _, k := range allSubsKeys {
		allSmembersCmds = append(allSmembersCmds, builder.Smembers().Key(k).Build())
	}

	allSmembersResults := as.cache.DoMulti(ctx, allSmembersCmds...)
	for _, res := range allSmembersResults {
		subs, _ := res.AsStrSlice()
		if len(subs) > 0 {
			allEmpty = false
			break
		}
	}

	if allEmpty {
		_, _ = as.cache.SRem(ctx, AlarmChannelRegistryKey, []string{channelID})
	}
}
