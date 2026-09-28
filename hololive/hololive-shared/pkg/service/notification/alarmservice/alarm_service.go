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

	"github.com/kapu/hololive-shared/pkg/domain"
)

// GetRoomAlarms는 방이 구독한 채널 ID를 중복 없이 돌려준다. 같은 채널의 UNIT B 멤버 구독 행은 채널 하나로 합친다.
func (as *AlarmService) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarms, err := as.GetRoomAlarmsWithTypes(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("get room alarms: %w", err)
	}

	channelIDs := uniqueAlarmChannelIDs(alarms)
	if channelIDs == nil {
		return []string{}, nil
	}

	return channelIDs, nil
}

func (as *AlarmService) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	alarms, err := as.alarmRepository.FindByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	return alarms, nil
}
