package alarmservice

import (
	"context"
	"time"
)

func (as *AlarmService) MarkAsNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) error {
	return as.cacheState.MarkAsNotified(ctx, streamID, startScheduled, minutesUntil)
}

func (as *AlarmService) WasNotified(ctx context.Context, streamID string, startScheduled time.Time, minutesUntil int) bool {
	return as.cacheState.WasNotified(ctx, streamID, startScheduled, minutesUntil)
}
