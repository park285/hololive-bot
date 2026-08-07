package dispatchrun

import (
	"fmt"
	"sort"

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
	grouped := groupAlarmDispatchEnvelopesByKey(envelopes, alarmDispatchKaringGroupKey)
	split := make([]alarmDispatchGroup, 0, len(grouped))
	for i := range grouped {
		split = append(split, splitAlarmDispatchKaringGroup(grouped[i])...)
	}
	return split
}

// 한 그룹이 여러 chunk로 나뉘면 앞 chunk만 전송된 뒤 뒤 chunk가 502로 실패하는 부분 성공이 가능하고,
// 그 실패는 not-admitted라 envelopeCount와 무관하게 재시도되어 전체를 retry-solo로 재그룹한다 —
// 이미 전송된 item이 다른 ClientRequestID로 다시 나간다. chunk 경계에서 미리 잘라 그 상태를 없앤다.
func splitAlarmDispatchKaringGroup(group alarmDispatchGroup) []alarmDispatchGroup {
	if len(group.envelopes) <= alarmDispatchKaringMaxItemsPerRequest ||
		len(group.envelopes) != len(group.notifications) {
		return []alarmDispatchGroup{group}
	}

	order := make([]int, len(group.envelopes))
	for i := range order {
		order[i] = i
	}
	// buildAlarmDispatchKaringContentListRequests와 같은 정렬이어야 분할 결과가 기존 chunk 경계와
	// 일치하고, 드레인 순서가 ClientRequestID에 새지 않는다.
	sort.SliceStable(order, func(a, b int) bool {
		return alarmDispatchNotificationKaringItemIdentity(group, order[a]) <
			alarmDispatchNotificationKaringItemIdentity(group, order[b])
	})

	groups := make([]alarmDispatchGroup, 0, (len(order)+alarmDispatchKaringMaxItemsPerRequest-1)/alarmDispatchKaringMaxItemsPerRequest)
	for start := 0; start < len(order); start += alarmDispatchKaringMaxItemsPerRequest {
		end := min(start+alarmDispatchKaringMaxItemsPerRequest, len(order))
		sub := alarmDispatchGroup{roomID: group.roomID, minutesUntil: group.minutesUntil}
		for _, index := range order[start:end] {
			envelope := group.envelopes[index]
			sub.envelopes = append(sub.envelopes, envelope)
			sub.notifications = append(sub.notifications, group.notifications[index])
		}
		groups = append(groups, sub)
	}
	return groups
}

func groupAlarmDispatchEnvelopesByKey(
	envelopes []domain.AlarmQueueEnvelope,
	keyFunc func(*domain.AlarmQueueEnvelope) string,
) []alarmDispatchGroup {
	groups := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}
	for i := range envelopes {
		envelope := &envelopes[i]
		key := alarmDispatchRegroupKey(envelope, keyFunc)
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

// 재드레인 봉투가 신규 봉투와 병합되면 그룹 구성이 바뀌어 ClientRequestID가 다르게 재파생되고,
// 이미 admission된 첫 발송이 dedup에 접히지 않아 중복 발화한다 — 재시도 봉투는 항상 solo 그룹.
func alarmDispatchRegroupKey(envelope *domain.AlarmQueueEnvelope, keyFunc func(*domain.AlarmQueueEnvelope) string) string {
	if envelope != nil && envelope.Retry != nil && envelope.Retry.Attempt > 0 {
		return fmt.Sprintf("retry-solo|%d", envelope.DispatchOutboxID)
	}
	return keyFunc(envelope)
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
