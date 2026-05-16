package polling

import (
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type NotificationRouteRequest struct {
	AlarmType   domain.AlarmType
	ChannelID   string
	PublishedAt time.Time
}

type NotificationRouteDecider func(NotificationRouteRequest) bool
