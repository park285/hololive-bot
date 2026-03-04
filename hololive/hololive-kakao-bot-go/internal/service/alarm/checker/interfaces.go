package checker

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/domain"
)

// Runner는 플랫폼별 알람 체커 공통 실행 계약이다.
type Runner interface {
	Check(ctx context.Context) ([]*domain.AlarmNotification, error)
}

// SendResult는 알림 큐 발행 집계 결과다.
type SendResult struct {
	Sent    int
	Skipped int
	Failed  int
}

// Sender는 알림 발행기(Notifier) 계약이다.
type Sender interface {
	Send(ctx context.Context, notifications []*domain.AlarmNotification) (SendResult, error)
}
