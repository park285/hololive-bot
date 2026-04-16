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
	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarmkeys "github.com/kapu/hololive-shared/pkg/service/alarm/keys"
)

// getAlarmKey: 방 기반 알람 키 (room_id가 PRIMARY).
func (as *AlarmService) getAlarmKey(roomID string) string {
	return sharedalarmkeys.BuildRoomAlarmKey(roomID)
}

// getRegistryKey: 방 기반 레지스트리 키 (room_id가 PRIMARY).
func (as *AlarmService) getRegistryKey(roomID string) string {
	return roomID
}

func (as *AlarmService) channelSubscribersKey(channelID string) string {
	return sharedalarmkeys.BuildChannelSubscriberKey(channelID, domain.AlarmTypeLive)
}

func (as *AlarmService) channelSubscribersKeyByType(channelID string, alarmType domain.AlarmType) string {
	return sharedalarmkeys.BuildChannelSubscriberKey(channelID, alarmType)
}
