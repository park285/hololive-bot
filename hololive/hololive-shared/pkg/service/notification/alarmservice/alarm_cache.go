package alarmservice

import (
	"context"
	"fmt"
	"strings"

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

// resolveCacheMemberName은 멤버 데이터 표시명을 우선하고, 없으면 호출자 값(알람 등록 때 member_name)을 쓴다. 이 단계는
// alarm.Repository의 memberDisplayNameExceptionContract 예외 계약에 속하며 hololive_alarm_member_name_caller_fallback_total로 센다.
func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	if name := as.cacheState.ResolveMemberDataName(ctx, channelID); name != "" {
		return name
	}

	name := strings.TrimSpace(fallback)
	if name != "" {
		observeAlarmMemberNameCallerFallback()
	}

	return name
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	out, err := as.cacheState.GetChannelSubscribersByType(ctx, channelID, alarmType)
	if err != nil {
		return out, fmt.Errorf("get channel subscribers by type: %w", err)
	}

	return out, nil
}
