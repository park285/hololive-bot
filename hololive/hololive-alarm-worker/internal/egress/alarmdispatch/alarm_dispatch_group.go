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
	envelopeErr   error
}

func groupAlarmDispatchEnvelopesForDelivery(envelopes []dispatchoutbox.AlarmQueueEnvelope) []alarmDispatchGroup {
	grouped := make([]alarmDispatchGroup, 0, len(envelopes))
	index := map[string]int{}

	for i := range envelopes {
		envelope := &envelopes[i]
		envelopeErr := alarmDispatchEnvelopeError(envelope)
		key := fmt.Sprintf("send-unit|%d", envelope.SendUnitID)

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
	case domain.AlarmDispatchSourceKindEventGrade:
		if err := envelope.ValidateCanonicalDispatch(); err != nil {
			return fmt.Errorf("validate event grade dispatch: %w", err)
		}

		return nil
	case domain.AlarmDispatchSourceKindCelebration:
		if envelope.Celebration != nil && !envelope.Celebration.Kind.IsValid() {
			return fmt.Errorf("celebration kind %q has no egress path", envelope.Celebration.Kind)
		}

		return nil
	case domain.AlarmDispatchSourceKindXSpace:
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
