package youtubedispatch

import (
	"sync"
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type claimedDelivery struct {
	row    domain.YouTubeNotificationDelivery
	outbox domain.YouTubeNotificationOutbox
}

type deliveryAttempt struct {
	members       []claimedDelivery
	request       deliverySendRequest
	operation     store.StartedOperation
	irisRequestID string

	sendingStartedAt time.Time

	rows     []domain.YouTubeNotificationDelivery
	outboxes []domain.YouTubeNotificationOutbox
}

func newDeliveryAttempt(members []claimedDelivery) deliveryAttempt {
	a := deliveryAttempt{
		members:  members,
		rows:     make([]domain.YouTubeNotificationDelivery, len(members)),
		outboxes: make([]domain.YouTubeNotificationOutbox, len(members)),
	}
	for i := range members {
		a.rows[i] = members[i].row
		a.outboxes[i] = members[i].outbox
	}

	return a
}

func (a *deliveryAttempt) singleton(i int) deliveryAttempt {
	return deliveryAttempt{
		members:  a.members[i : i+1],
		rows:     a.rows[i : i+1],
		outboxes: a.outboxes[i : i+1],
	}
}

type deliveryDispatchResult struct {
	DispatchResult

	mu sync.Mutex
}

func newDeliveryDispatchResult(capacity int) deliveryDispatchResult {
	return deliveryDispatchResult{
		SuccessDeliveryIDs: make([]int64, 0, capacity),
		TouchedOutboxIDs:   make([]int64, 0, capacity),
	}
}
