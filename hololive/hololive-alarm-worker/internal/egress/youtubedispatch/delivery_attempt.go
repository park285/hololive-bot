package youtubedispatch

import (
	"sync"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubedispatch/store"
	"github.com/kapu/hololive-shared/pkg/domain"
)

// claimedDelivery는 claim된 발송 행과 해당 outbox를 함께 유지한다.
type claimedDelivery struct {
	row    domain.YouTubeNotificationDelivery
	outbox domain.YouTubeNotificationOutbox
}

// deliveryAttempt의 요청은 모든 members가 공유한다. 고정 뒤에는 저장된 요청만
// 사용하고, singleton fallback은 원래 member와 그 소유권으로 별도 시도를 만든다.
// 필드 irisRequestID는 발송이 성공했을 때 Iris 202가 돌려준 requestId이며 로그에 남기지 않는다.
type deliveryAttempt struct {
	members       []claimedDelivery
	request       deliverySendRequest
	operation     store.StartedOperation
	irisRequestID string

	// 기존 영속 전이·formatter·audit API에 넘기는 읽기 전용 투영이다.
	// 생성 때 한 번 만들고 singleton은 같은 backing array를 사용한다.
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

// 결과와 그 잠금은 같은 배치에 속하며 작업 goroutine 종료 뒤에만 결과를 반환한다.
type deliveryDispatchResult struct {
	DispatchResult

	mu sync.Mutex
}

func newDeliveryDispatchResult(capacity int) deliveryDispatchResult {
	return deliveryDispatchResult{
		SuccessDeliveryIDs: make([]int64, 0, capacity),
		TouchedOutboxIDs:   make([]int64, 0, capacity),
		FailureBuckets:     make(map[string][]int64),
	}
}
