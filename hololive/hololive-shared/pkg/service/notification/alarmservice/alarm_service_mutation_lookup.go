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
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) findAlarmRecordForMutation(ctx context.Context, roomID, channelID string) (*domain.Alarm, error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)
	if roomID == "" || channelID == "" {
		return nil, nil
	}

	if as.alarmRepository != nil {
		return as.findAlarmRecordForMutationFromRepository(ctx, roomID, channelID)
	}

	return as.findAlarmRecordForMutationFromCache(ctx, roomID, channelID)
}

func (as *AlarmService) findAlarmRecordForMutationFromRepository(ctx context.Context, roomID, channelID string) (*domain.Alarm, error) {
	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	for _, alarm := range alarms {
		if alarm == nil || strings.TrimSpace(alarm.ChannelID) != channelID {
			continue
		}
		cloned := *alarm
		return &cloned, nil
	}

	return nil, nil
}

func (as *AlarmService) findAlarmRecordForMutationFromCache(ctx context.Context, roomID, channelID string) (*domain.Alarm, error) {
	exists, err := as.cache.SIsMember(ctx, as.getAlarmKey(roomID), channelID)
	if err != nil {
		return nil, fmt.Errorf("check room alarm membership: %w", err)
	}
	if !exists {
		return nil, nil
	}

	registryKey := as.getRegistryKey(roomID)
	currentTypes, err := as.currentCachedAlarmTypes(ctx, channelID, registryKey)
	if err != nil {
		return nil, err
	}

	return &domain.Alarm{
		RoomID:     roomID,
		ChannelID:  channelID,
		AlarmTypes: currentTypes,
	}, nil
}

func (as *AlarmService) currentCachedAlarmTypes(ctx context.Context, channelID, registryKey string) (domain.AlarmTypes, error) {
	currentTypes := make(domain.AlarmTypes, 0, len(domain.AllAlarmTypes))
	for _, alarmType := range domain.AllAlarmTypes {
		subscriberKey := as.channelSubscribersKeyByType(channelID, alarmType)
		isSubscriber, err := as.cache.SIsMember(ctx, subscriberKey, registryKey)
		if err != nil {
			return nil, fmt.Errorf("check subscriber type %s: %w", alarmType, err)
		}
		if isSubscriber {
			currentTypes = append(currentTypes, alarmType)
		}
	}
	if len(currentTypes) == 0 {
		currentTypes = append(domain.AlarmTypes(nil), domain.DefaultAlarmTypes...)
	}

	return currentTypes, nil
}

func (as *AlarmService) loadRoomAlarmsForMutation(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepository != nil {
		alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
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
