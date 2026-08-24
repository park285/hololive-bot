package alarmservice

import (
	"context"
	"fmt"
	"time"
)

func (as *AlarmService) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	if err := as.cacheState.MarkAsNotified(ctx, streamID, startScheduled, minutesUntil); err != nil {
		return fmt.Errorf("mark as notified: %w", err)
	}

	return nil
}

func (as *AlarmService) WasNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) bool {
	return as.cacheState.WasNotified(ctx, streamID, startScheduled, minutesUntil)
}
