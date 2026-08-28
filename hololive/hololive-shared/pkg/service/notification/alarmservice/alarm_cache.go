package alarmservice

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmcache"
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

func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	return as.cacheState.ResolveCacheMemberName(ctx, channelID, fallback)
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	out, err := as.cacheState.GetChannelSubscribersByType(ctx, channelID, alarmType)
	if err != nil {
		return out, fmt.Errorf("get channel subscribers by type: %w", err)
	}

	return out, nil
}

func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	as.cacheMutationMu.Lock()
	defer as.cacheMutationMu.Unlock()

	if err := as.cacheState.SetRoomName(ctx, roomID, roomName); err != nil {
		return fmt.Errorf("set room name: %w", err)
	}

	return nil
}

func (as *AlarmService) SetUserName(ctx context.Context, userID, userName string) error {
	as.cacheMutationMu.Lock()
	defer as.cacheMutationMu.Unlock()

	if err := as.cacheState.SetUserName(ctx, userID, userName); err != nil {
		return fmt.Errorf("set user name: %w", err)
	}

	return nil
}

func normalizeScheduledMinute(startScheduled time.Time) time.Time {
	return alarmcache.NormalizeScheduledMinute(startScheduled)
}
