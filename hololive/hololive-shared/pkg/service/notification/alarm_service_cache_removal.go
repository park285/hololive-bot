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
	"fmt"
	"log/slog"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) removeAlarmFromCache(
	ctx context.Context,
	roomID string,
	channelID string,
	alarmTypes domain.AlarmTypes,
	removeRoomChannel bool,
) (bool, error) {
	alarmKey := as.getAlarmKey(roomID)
	removedRoomChannel := int64(0)

	if removeRoomChannel {
		removed, err := as.cache.SRem(ctx, alarmKey, []string{channelID})
		if err != nil {
			return false, fmt.Errorf("remove room alarm: %w", err)
		}
		removedRoomChannel = removed
	}

	registryKey := as.getRegistryKey(roomID)
	if err := as.removeChannelSubscribers(ctx, channelID, registryKey, alarmTypes); err != nil {
		return false, fmt.Errorf("remove channel subscribers: %w", err)
	}

	if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil {
		return false, fmt.Errorf("cleanup channel registry if empty: %w", err)
	}

	if removeRoomChannel {
		remainingAlarms, err := as.cache.SMembers(ctx, alarmKey)
		if err != nil {
			return false, fmt.Errorf("read remaining room alarms: %w", err)
		}

		if len(remainingAlarms) == 0 {
			if _, err := as.cache.SRem(ctx, AlarmRegistryKey, []string{registryKey}); err != nil {
				return false, fmt.Errorf("remove room registry: %w", err)
			}

			if as.logger != nil {
				as.logger.Info("Room removed from registry (no alarms left)", slog.String("room_id", roomID))
			}
		}
	}

	return removedRoomChannel > 0 || len(alarmTypes) > 0, nil
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
