package workerapp

import (
	"fmt"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type alarmDispatchGroup struct {
	roomID        string
	minutesUntil  int
	envelopes     []domain.AlarmQueueEnvelope
	notifications []domain.AlarmNotification
}

func groupAlarmDispatchEnvelopes(envelopes []domain.AlarmQueueEnvelope) []alarmDispatchGroup {
	return groupAlarmDispatchEnvelopesByKey(envelopes, alarmDispatchGroupKey)
}

func groupAlarmDispatchEnvelopesForKaring(envelopes []domain.AlarmQueueEnvelope, karingEnabled bool) []alarmDispatchGroup {
	if !karingEnabled {
		return groupAlarmDispatchEnvelopes(envelopes)
	}
	return groupAlarmDispatchEnvelopesByKey(envelopes, alarmDispatchKaringGroupKey)
}

func groupAlarmDispatchEnvelopesByKey(
	envelopes []domain.AlarmQueueEnvelope,
	keyFunc func(*domain.AlarmQueueEnvelope) string,
) []alarmDispatchGroup {
	groups := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}
	for i := range envelopes {
		envelope := &envelopes[i]
		key := keyFunc(envelope)
		groupIndex, ok := index[key]
		if !ok {
			index[key] = len(groups)
			groups = append(groups, newAlarmDispatchGroup(envelope))
			continue
		}
		appendAlarmDispatchEnvelope(&groups[groupIndex], envelope)
	}
	return groups
}

func newAlarmDispatchGroup(envelope *domain.AlarmQueueEnvelope) alarmDispatchGroup {
	if envelope == nil {
		return alarmDispatchGroup{}
	}
	return alarmDispatchGroup{
		roomID:        envelope.Notification.RoomID,
		minutesUntil:  envelope.Notification.MinutesUntil,
		envelopes:     []domain.AlarmQueueEnvelope{*envelope},
		notifications: []domain.AlarmNotification{envelope.Notification},
	}
}

func appendAlarmDispatchEnvelope(group *alarmDispatchGroup, envelope *domain.AlarmQueueEnvelope) {
	if group == nil || envelope == nil {
		return
	}
	group.minutesUntil = minAlarmDispatchMinutes(group.minutesUntil, envelope.Notification.MinutesUntil)
	group.envelopes = append(group.envelopes, *envelope)
	group.notifications = append(group.notifications, envelope.Notification)
}

func alarmDispatchGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope == nil {
		return ""
	}
	if key, ok := alarmDispatchSourceGroupKey(envelope); ok {
		return key
	}
	return alarmDispatchTimeGroupKey(envelope)
}

func alarmDispatchSourceGroupKey(envelope *domain.AlarmQueueEnvelope) (string, bool) {
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		return alarmDispatchCelebrationGroupKey(envelope), true
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return fmt.Sprintf("%s|source|%s|%s|%s|%s",
			envelope.Notification.RoomID,
			envelope.SourceKind,
			envelope.YouTubeOutbox.ChannelID,
			envelope.YouTubeOutbox.Kind,
			envelope.YouTubeOutbox.Identity(),
		), true
	}
	return "", false
}

func alarmDispatchCelebrationGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	key := fmt.Sprintf("%s|celebration|%s|%s",
		envelope.Notification.RoomID,
		envelope.Celebration.Kind,
		envelope.Celebration.ChannelID,
	)
	if envelope.Celebration.VideoID != "" {
		key += "|" + envelope.Celebration.VideoID
	}
	return key
}

func alarmDispatchTimeGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope.Notification.Stream != nil && envelope.Notification.Stream.StartScheduled != nil {
		minuteBucket := envelope.Notification.Stream.StartScheduled.UTC().Unix() / 60
		return fmt.Sprintf("%s|scheduled|%d", envelope.Notification.RoomID, minuteBucket)
	}
	return fmt.Sprintf("%s|minutes|%d", envelope.Notification.RoomID, envelope.Notification.MinutesUntil)
}

func alarmDispatchKaringGroupKey(envelope *domain.AlarmQueueEnvelope) string {
	if envelope == nil {
		return ""
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindCelebration && envelope.Celebration != nil {
		return alarmDispatchGroupKey(envelope)
	}
	if envelope.SourceKind == domain.AlarmDispatchSourceKindYouTubeOutbox && envelope.YouTubeOutbox != nil {
		return alarmDispatchGroupKey(envelope)
	}
	phase := "prelive"
	if alarmDispatchNotificationIsStarting(&envelope.Notification) {
		phase = "starting"
	}
	return fmt.Sprintf(
		"%s|karing|%s|%s|minutes|%d",
		envelope.Notification.RoomID,
		envelope.Notification.AlarmType,
		phase,
		envelope.Notification.MinutesUntil,
	)
}

func minAlarmDispatchMinutes(current, next int) int {
	if next < 0 {
		return current
	}
	if current < 0 || next < current {
		return next
	}
	return current
}
