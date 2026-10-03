package subscriptions

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (as *AlarmService) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	if err := as.cacheState.CacheMemberName(ctx, channelID, memberName); err != nil {
		return fmt.Errorf("cache member name: %w", err)
	}

	return nil
}

func (as *AlarmService) GetMemberName(ctx context.Context, channelID string) (string, error) {
	out, err := as.cacheState.GetMemberName(ctx, channelID)
	if err != nil {
		return out, fmt.Errorf("get member name: %w", err)
	}

	return out, nil
}

// resolveCacheMemberName은 members 정본의 한국어 표시명을 캐시에 쓴다. 표시명이 없으면 빈 값이며, 알림 표시 단계가
// misc/vtuber_fallback 문구를 쓴다.
func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID string) string {
	return as.cacheState.ResolveMemberDataName(ctx, channelID)
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	out, err := as.cacheState.GetChannelSubscribersByType(ctx, channelID, alarmType)
	if err != nil {
		return out, fmt.Errorf("get channel subscribers by type: %w", err)
	}

	return out, nil
}
