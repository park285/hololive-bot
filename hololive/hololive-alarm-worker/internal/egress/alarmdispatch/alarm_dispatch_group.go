package alarmdispatch

import (
	"errors"
	"fmt"

	"github.com/kapu/hololive-alarm-worker/internal/service/alarm/dispatchoutbox"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type alarmDispatchGroup struct {
	request       *dispatchoutbox.SendRequest
	roomID        string
	minutesUntil  int
	envelopes     []dispatchoutbox.AlarmQueueEnvelope
	notifications []domain.AlarmNotification
	// envelopeErr는 발송할 수 없는 봉투(퇴역 제공자, 대상 없음, 미지원 source)의 사유다. 이런 봉투는 발송 전 실패로 라우팅한다.
	envelopeErr error
}

// 모든 봉투를 Text 경로로 보낸다. 오픈채팅은 sender가 Markdown으로 전송한다. 저장된 send unit이 있는 delivery만 claim되므로
// send unit이 발송 경계다.
func groupAlarmDispatchEnvelopesForDelivery(envelopes []dispatchoutbox.AlarmQueueEnvelope) []alarmDispatchGroup {
	grouped := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}

	for i := range envelopes {
		envelope := &envelopes[i]
		envelopeErr := alarmDispatchEnvelopeError(envelope)
		key := fmt.Sprintf("send-unit|%d", envelope.SendUnitID)

		// 발송할 수 없는 봉투는 같은 send unit의 발송 가능한 봉투와 섞이지 않게 별도 그룹으로 둔다.
		if envelopeErr != nil {
			key = "invalid|" + key
		}

		groupIndex, ok := index[key]
		if !ok {
			group := newAlarmDispatchGroup(envelope)

			group.envelopeErr = envelopeErr
			index[key] = len(grouped)
			grouped = append(grouped, group)

			continue
		}

		appendAlarmDispatchEnvelope(&grouped[groupIndex], envelope)
	}

	return grouped
}

func newAlarmDispatchGroup(envelope *dispatchoutbox.AlarmQueueEnvelope) alarmDispatchGroup {
	if envelope == nil {
		return alarmDispatchGroup{}
	}

	return alarmDispatchGroup{
		roomID:        envelope.Notification.RoomID,
		minutesUntil:  envelope.Notification.MinutesUntil,
		envelopes:     []dispatchoutbox.AlarmQueueEnvelope{*envelope},
		notifications: []domain.AlarmNotification{envelope.Notification},
	}
}

func appendAlarmDispatchEnvelope(group *alarmDispatchGroup, envelope *dispatchoutbox.AlarmQueueEnvelope) {
	if group == nil || envelope == nil {
		return
	}

	group.minutesUntil = minAlarmDispatchMinutes(group.minutesUntil, envelope.Notification.MinutesUntil)
	group.envelopes = append(group.envelopes, *envelope)
	group.notifications = append(group.notifications, envelope.Notification)
}

func alarmDispatchEnvelopeError(envelope *dispatchoutbox.AlarmQueueEnvelope) error {
	if envelope == nil {
		return errors.New("alarm dispatch envelope is nil")
	}

	switch envelope.SourceKind {
	case domain.AlarmDispatchSourceKindCelebration, domain.AlarmDispatchSourceKindXSpace:
		return nil
	case "":
		return alarmDispatchStreamEnvelopeError(envelope)
	default:
		return fmt.Errorf("alarm dispatch source kind %q has no egress path", envelope.SourceKind)
	}
}

func alarmDispatchStreamEnvelopeError(envelope *dispatchoutbox.AlarmQueueEnvelope) error {
	stream := envelope.Notification.Stream
	if stream == nil {
		return errors.New("alarm notification stream is nil")
	}

	if !stream.HasYouTubeInfo() {
		return errors.New("alarm notification has no YouTube target")
	}

	return nil
}

func alarmDispatchGroupError(group alarmDispatchGroup) error {
	if group.envelopeErr != nil {
		return group.envelopeErr
	}

	if len(group.envelopes) == 0 {
		return errors.New("alarm dispatch group is empty")
	}

	return nil
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
