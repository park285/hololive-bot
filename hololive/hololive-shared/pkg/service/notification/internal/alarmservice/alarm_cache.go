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
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	sharedalarm "github.com/kapu/hololive-shared/pkg/service/alarm"
	"github.com/park285/shared-go/pkg/stringutil"
)

func (as *AlarmService) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	if err := as.cache.HSet(ctx, MemberNameKey, channelID, memberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	return nil
}

func (as *AlarmService) GetMemberName(ctx context.Context, channelID string) (string, error) {
	name, err := as.cache.HGet(ctx, MemberNameKey, channelID)
	if err != nil {
		return "", fmt.Errorf("get member name: %w", err)
	}

	return name, nil
}

func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	if name := as.resolveMemberDataName(ctx, channelID); name != "" {
		return name
	}
	return stringutil.TrimSpace(fallback)
}

func (as *AlarmService) resolveMemberDataName(ctx context.Context, channelID string) string {
	provider := as.memberData
	if provider == nil {
		return ""
	}
	if scoped := provider.WithContext(ctx); scoped != nil {
		provider = scoped
	}
	member := provider.FindMemberByChannelID(channelID)
	if member == nil {
		return ""
	}
	return firstMemberName(member.ShortKoreanName, member.NameKo, member.Name)
}

func firstMemberName(candidates ...string) string {
	for _, candidate := range candidates {
		if name := stringutil.TrimSpace(candidate); name != "" {
			return name
		}
	}
	return ""
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	subscribers, err := sharedalarm.LookupChannelSubscribersByType(ctx, as.cache, channelID, alarmType)
	if err != nil {
		return nil, fmt.Errorf("lookup channel subscribers by type: %w", err)
	}

	return subscribers, nil
}

func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	if err := as.cache.HSet(ctx, RoomNamesCacheKey, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}

func (as *AlarmService) SetUserName(ctx context.Context, userID, userName string) error {
	if err := as.cache.HSet(ctx, UserNamesCacheKey, userID, userName); err != nil {
		return fmt.Errorf("set user name: %w", err)
	}

	return nil
}

func normalizeScheduledMinute(startScheduled time.Time) time.Time {
	return startScheduled.Truncate(time.Minute)
}
