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
	"log/slog"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// 멤버 표시명만 subscriber cache의 member name hash에서 읽고, 그 조회 실패는 이름 없이 목록을 낸다.
func (as *AlarmService) GetAllAlarmKeys(ctx context.Context) ([]*domain.AlarmEntry, error) {
	entries, err := as.alarmRepository.ListAlarmEntries(ctx)
	if err != nil {
		return nil, fmt.Errorf("list alarm entries: %w", err)
	}

	if len(entries) == 0 {
		return []*domain.AlarmEntry{}, nil
	}

	channelIDs := make([]string, 0, len(entries))
	for _, entry := range entries {
		if entry.RoomName == "" {
			entry.RoomName = entry.RoomID
		}

		channelIDs = append(channelIDs, entry.ChannelID)
	}

	memberNames, err := as.getMemberNamesBatch(ctx, channelIDs)
	if err != nil {
		as.logger.Warn("failed to load member names", slog.Any("error", err))

		memberNames = map[string]string{}
	}

	for _, entry := range entries {
		entry.MemberName = memberNames[entry.ChannelID]
	}

	return entries, nil
}

func (as *AlarmService) CountAlarmEntries(ctx context.Context) (int, error) {
	count, err := as.alarmRepository.CountAlarmEntries(ctx)
	if err != nil {
		return 0, fmt.Errorf("count alarm entries: %w", err)
	}

	return count, nil
}

// 공백뿐인 이름은 지정을 해제해 관리 목록이 Kakao 방 이름으로 돌아간다.
func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	roomID = strings.TrimSpace(roomID)
	if roomID == "" {
		return errors.New("set room name: room id is required")
	}

	roomName = strings.TrimSpace(roomName)
	if roomName == "" {
		if err := as.alarmRepository.DeleteRoomDisplayName(ctx, roomID); err != nil {
			return fmt.Errorf("clear room name: %w", err)
		}

		return nil
	}

	if err := as.alarmRepository.SetRoomDisplayName(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}
