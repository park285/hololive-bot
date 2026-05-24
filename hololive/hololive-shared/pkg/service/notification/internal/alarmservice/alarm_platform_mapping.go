package alarmservice

import (
	"context"
)

func (as *AlarmService) SyncPlatformMappings(ctx context.Context) error {
	return as.platformMapper.SyncAll(ctx)
}

func (as *AlarmService) syncPlatformMappingForChannel(ctx context.Context, channelID string) error {
	return as.platformMapper.SyncForChannel(ctx, channelID)
}
