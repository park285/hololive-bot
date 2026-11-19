package checking

import (
	"context"
	"fmt"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func (c *YouTubeChecker) buildChannelNotifications(
	ctx context.Context,
	targetPolicy targetpolicy.TargetMinutePolicy,
	channelID string,
	subscriberRooms []string,
	streams []*domain.Stream,
	window targetpolicy.EvaluationWindow,
	now time.Time,
	settledRoomsByStreamID map[string]map[string]struct{},
	liveObservedAtByStreamID ...map[string]time.Time,
) ([]*domain.AlarmNotification, error) {
	notifications := make([]*domain.AlarmNotification, 0, len(streams)*len(subscriberRooms))
	for _, stream := range streams {
		if stream == nil {
			continue
		}

		streamRooms, err := c.eventSubscriberRooms(ctx, channelID, stream.Title, subscriberRooms)
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: %w", err)
		}

		upcomingNotifications, err := c.buildUpcomingNotifications(ctx, targetPolicy, stream, streamRooms, window)
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build upcoming notifications: %w", err)
		}

		notifications = append(notifications, upcomingNotifications...)

		liveCatchupNotifications, err := c.buildLiveCatchupNotifications(ctx, targetPolicy, channelID, stream, streamRooms, now, settledRoomsByStreamID[stream.ID], liveObservedAt(stream, liveObservedAtByStreamID...))
		if err != nil {
			return nil, fmt.Errorf("build channel notifications: build live catchup notifications: %w", err)
		}

		notifications = append(notifications, liveCatchupNotifications...)
	}

	return notifications, nil
}

func appendYouTubeChannelNotifications(
	mu *sync.Mutex,
	notifications *[]*domain.AlarmNotification,
	channelNotifications []*domain.AlarmNotification,
) {
	if len(channelNotifications) == 0 {
		return
	}

	mu.Lock()

	*notifications = append(*notifications, channelNotifications...)
	mu.Unlock()
}

func resolveLiveStart(stream *domain.Stream) *time.Time {
	if stream == nil {
		return nil
	}

	if stream.StartActual != nil && !stream.StartActual.IsZero() {
		start := stream.StartActual.UTC()
		return &start
	}

	if stream.StartScheduled != nil && !stream.StartScheduled.IsZero() {
		start := stream.StartScheduled.UTC()
		return &start
	}

	return nil
}

func groupStreamsByChannel(streams []*domain.Stream) map[string][]*domain.Stream {
	grouped := make(map[string][]*domain.Stream)

	for _, stream := range streams {
		if stream == nil {
			continue
		}

		channelID := stream.ChannelID
		if channelID == "" && stream.Channel != nil {
			channelID = stream.Channel.ID
		}

		if channelID == "" {
			continue
		}

		grouped[channelID] = append(grouped[channelID], stream)
	}

	return grouped
}

func youtubeStreamChannelID(stream *domain.Stream) string {
	if stream == nil {
		return ""
	}

	if stream.ChannelID != "" {
		return stream.ChannelID
	}

	if stream.Channel != nil {
		return stream.Channel.ID
	}

	return ""
}
