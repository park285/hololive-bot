package alarmservice

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmcache"
)

func (as *AlarmService) CacheMemberName(ctx context.Context, channelID, memberName string) error {
	return as.cacheState.CacheMemberName(ctx, channelID, memberName)
}

func (as *AlarmService) GetMemberName(ctx context.Context, channelID string) (string, error) {
	return as.cacheState.GetMemberName(ctx, channelID)
}

func (as *AlarmService) resolveCacheMemberName(ctx context.Context, channelID, fallback string) string {
	return as.cacheState.ResolveCacheMemberName(ctx, channelID, fallback)
}

func (as *AlarmService) resolveMemberDataName(ctx context.Context, channelID string) string {
	return as.cacheState.ResolveMemberDataName(ctx, channelID)
}

func firstMemberName(candidates ...string) string {
	return alarmcache.FirstMemberName(candidates...)
}

func (as *AlarmService) GetChannelSubscribersByType(ctx context.Context, channelID string, alarmType domain.AlarmType) ([]string, error) {
	return as.cacheState.GetChannelSubscribersByType(ctx, channelID, alarmType)
}

func (as *AlarmService) SetRoomName(ctx context.Context, roomID, roomName string) error {
	return as.cacheState.SetRoomName(ctx, roomID, roomName)
}

func (as *AlarmService) SetUserName(ctx context.Context, userID, userName string) error {
	return as.cacheState.SetUserName(ctx, userID, userName)
}

func normalizeScheduledMinute(startScheduled time.Time) time.Time {
	return alarmcache.NormalizeScheduledMinute(startScheduled)
}
