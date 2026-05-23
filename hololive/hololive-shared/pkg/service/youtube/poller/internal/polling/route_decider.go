package polling

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type NotificationRouteRequest struct {
	AlarmType   domain.AlarmType
	ChannelID   string
	PublishedAt time.Time
}

type NotificationRouteDecider func(NotificationRouteRequest) bool

func ShouldEnqueueRoutedNotification(
	routeDecider NotificationRouteDecider,
	alarmType domain.AlarmType,
	channelID string,
	publishedAt time.Time,
) bool {
	if routeDecider == nil {
		return true
	}
	return routeDecider(NotificationRouteRequest{
		AlarmType:   alarmType,
		ChannelID:   channelID,
		PublishedAt: yttimestamp.Normalize(publishedAt),
	})
}
