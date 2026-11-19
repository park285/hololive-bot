package checking

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/alarmtiming/targetpolicy"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/delivery"
)

type Runner interface {
	Check(ctx context.Context, targetPolicy targetpolicy.TargetMinutePolicy) ([]*domain.AlarmNotification, error)
}

type Sender interface {
	Send(ctx context.Context, notifications []*domain.AlarmNotification, targetPolicy targetpolicy.TargetMinutePolicy) (delivery.SendResult, error)
}
