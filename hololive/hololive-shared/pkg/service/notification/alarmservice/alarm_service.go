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
	stdErrors "errors"
	"fmt"

	sharedlogging "github.com/park285/shared-go/pkg/logging"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) GetRoomAlarms(ctx context.Context, roomID string) ([]string, error) {
	alarmKey := as.getAlarmKey(roomID)

	channelIDs, err := as.cache.SMembers(ctx, alarmKey)
	if err != nil {
		return []string{}, sharedlogging.LogAndWrapError(ctx, as.logger, "get room alarms", err)
	}

	return channelIDs, nil
}

func (as *AlarmService) GetRoomAlarmsWithTypes(ctx context.Context, roomID string) ([]*domain.Alarm, error) {
	if as.alarmRepository == nil {
		return nil, stdErrors.New("alarm repository not configured")
	}

	alarms, err := as.alarmRepository.FindByRoom(ctx, roomID)
	if err != nil {
		return nil, fmt.Errorf("find room alarms: %w", err)
	}

	return alarms, nil
}
