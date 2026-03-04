package dispatch

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// NotificationGroup: room_id 단위 메시지 그룹.
type NotificationGroup struct {
	RoomID        string
	MinutesUntil  int
	Notifications []domain.AlarmNotification
	ClaimKeys     []string
}

// GroupEnvelopes: 큐 엔벨로프를 room/시각 기준으로 묶는다.
func GroupEnvelopes(envelopes []domain.AlarmQueueEnvelope) []NotificationGroup {
	if len(envelopes) == 0 {
		return nil
	}

	groups := make([]NotificationGroup, 0, len(envelopes))
	index := make(map[string]int, len(envelopes))

	for _, envelope := range envelopes {
		key := buildGroupKey(envelope.Notification)
		groupIndex, exists := index[key]
		if !exists {
			index[key] = len(groups)
			groups = append(groups, NotificationGroup{
				RoomID:        envelope.Notification.RoomID,
				MinutesUntil:  envelope.Notification.MinutesUntil,
				Notifications: []domain.AlarmNotification{envelope.Notification},
				ClaimKeys:     append([]string{}, envelope.ClaimKeys...),
			})
			continue
		}

		group := &groups[groupIndex]
		group.MinutesUntil = mergeMinutesUntil(group.MinutesUntil, envelope.Notification.MinutesUntil)
		group.Notifications = append(group.Notifications, envelope.Notification)
		group.ClaimKeys = append(group.ClaimKeys, envelope.ClaimKeys...)
	}

	return groups
}

func buildGroupKey(notification domain.AlarmNotification) string {
	if notification.Stream != nil && notification.Stream.StartScheduled != nil {
		minuteBucket := notification.Stream.StartScheduled.UTC().Unix() / 60
		return fmt.Sprintf("%s|scheduled|%d", notification.RoomID, minuteBucket)
	}
	return fmt.Sprintf("%s|minutes|%d", notification.RoomID, notification.MinutesUntil)
}

func mergeMinutesUntil(current, next int) int {
	if next < 0 {
		return current
	}
	if current < 0 {
		return next
	}
	if next < current {
		return next
	}
	return current
}
