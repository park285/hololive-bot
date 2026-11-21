package dispatchoutbox

import (
	"errors"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (e *AlarmQueueEnvelope) validateEventGrade() error {
	if e.Notification.AlarmType != domain.AlarmTypeEventGrade || e.Notification.RoomID == "" || !e.EventGrade.Valid() || e.XSpace != nil || e.Celebration != nil || e.Notification.Stream != nil || e.Notification.Channel != nil {
		return errors.New("invalid event grade dispatch")
	}

	return nil
}
