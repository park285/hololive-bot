package alarmservice

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/notification/internal/alarmcache"
)

func buildTitleFingerprint(title, streamID string) string {
	return alarmcache.BuildTitleFingerprint(title, streamID)
}

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
	return as.cacheState.MarkUpcomingEventNotified(ctx, roomID, channelID, stream)
}

func (as *AlarmService) WasUpcomingEventNotifiedRecently(
	ctx context.Context,
	roomID, channelID string,
	stream *domain.Stream,
	window time.Duration,
) bool {
	return as.cacheState.WasUpcomingEventNotifiedRecently(ctx, roomID, channelID, stream, window)
}
