package alarmservice

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/notification/alarmcache"
)

func resolveStreamChannelID(stream *domain.Stream, defaultChannelID string) string {
	return alarmcache.ResolveStreamChannelID(stream, defaultChannelID)
}

func (as *AlarmService) buildUpcomingEventKey(roomID, channelID, streamID, title string, startScheduled time.Time) string {
	return as.cacheState.BuildUpcomingEventKey(roomID, channelID, streamID, title, startScheduled)
}

func (as *AlarmService) MarkUpcomingEventNotified(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
) error {
	if err := as.cacheState.MarkUpcomingEventNotified(ctx, roomID, channelID, stream); err != nil {
		return fmt.Errorf("mark upcoming event notified: %w", err)
	}

	return nil
}
