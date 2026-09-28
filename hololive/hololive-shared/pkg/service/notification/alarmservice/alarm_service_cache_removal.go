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

func (as *AlarmService) removeAlarmFromCache(ctx context.Context, roomID, channelID string, alarmTypes domain.AlarmTypes) error {
	if err := as.removeChannelSubscribers(ctx, channelID, as.getRegistryKey(roomID), alarmTypes); err != nil {
		return fmt.Errorf("remove channel subscribers: %w", err)
	}

	if err := as.cleanupChannelRegistryIfEmpty(ctx, channelID); err != nil {
		return fmt.Errorf("cleanup channel registry if empty: %w", err)
	}

	return nil
}

func (as *AlarmService) clearRoomAlarmsFromCache(ctx context.Context, roomID string, channelIDs []string) error {
	if len(channelIDs) == 0 {
		return nil
	}

	if err := as.clearChannelSubscribersPipeline(ctx, channelIDs, as.getRegistryKey(roomID)); err != nil {
		return fmt.Errorf("clear channel subscribers pipeline: %w", err)
	}

	return nil
}
