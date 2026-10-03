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

package subscriptions

import (
	"context"
	"errors"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// errAlarmRecordNotFound는 대상 알람이 없다는 정상 결과이며, 호출부는 오류가 아닌 미존재로 다뤄야 한다.
var errAlarmRecordNotFound = errors.New("alarm record not found")

func (as *AlarmService) findAlarmRecordForMutation(ctx context.Context, roomID, channelID, hostID string) (*domain.Alarm, error) {
	roomID = strings.TrimSpace(roomID)
	channelID = strings.TrimSpace(channelID)

	if roomID == "" || channelID == "" {
		return nil, errAlarmRecordNotFound
	}

	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	for _, alarm := range alarms {
		if alarm == nil || strings.TrimSpace(alarm.ChannelID) != channelID || alarm.HostID != hostID {
			continue
		}

		cloned := *alarm

		return &cloned, nil
	}

	return nil, errAlarmRecordNotFound
}

func (as *AlarmService) loadRoomAlarmsForMutation(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	alarms, err := findRoomAlarmsFromRepository(ctx, as.alarmRepository, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
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
