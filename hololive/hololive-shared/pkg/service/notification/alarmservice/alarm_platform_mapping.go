package alarmservice

import (
	"context"
	"fmt"
)

func (as *AlarmService) SyncPlatformMappings(ctx context.Context) error {
	if err := as.platformMapper.SyncAll(ctx); err != nil {
		return fmt.Errorf("sync all: %w", err)
	}

	return nil
}

func (as *AlarmService) syncPlatformMappingForChannel(ctx context.Context, channelID string) error {
	if err := as.platformMapper.SyncForChannel(ctx, channelID); err != nil {
		return fmt.Errorf("sync platform mapping for channel: %w", err)
	}

	return nil
}
