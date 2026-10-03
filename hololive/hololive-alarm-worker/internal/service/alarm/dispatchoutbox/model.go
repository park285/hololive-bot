package dispatchoutbox

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Status string

// shadowed(비교 전용 행)는 v3 handoff와 함께 삭제했다(DEC-20260926-hololive-outbox-v3-convergence). DB CHECK에서도
// migration 226이 뺀다.
const (
	StatusPending     Status = "pending"
	StatusLeased      Status = "leased"
	StatusRetry       Status = "retry"
	StatusSending     Status = "sending"
	StatusSent        Status = "sent"
	StatusDLQ         Status = "dlq"
	StatusQuarantined Status = "quarantined"
	StatusCancelled   Status = "cancelled" //nolint:misspell // dispatch_delivery.status에 저장되는 실제 값이라, canceled로 바꾸면 기존 행과 상태 비교가 어긋난다.
)

type InsertResult string

const (
	Inserted          InsertResult = "inserted"
	DuplicateActive   InsertResult = "duplicate_active"
	DuplicateTerminal InsertResult = "duplicate_terminal"
)

type Record struct {
	ID               int64
	EventID          int64
	DedupeKey        string
	EventKey         string
	PayloadHash      string
	RoomID           string
	ChannelID        string
	AlarmType        domain.AlarmType
	Category         string
	Payload          []byte
	ClaimKeys        []string
	DeliveryContext  []byte
	DispatchGroupKey string
	SendUnitID       int64
	ClientRequestID  string
	Status           Status
	AttemptCount     int
	NextAttemptAt    time.Time
	LockedBy         string
	LockedAt         *time.Time
	LockExpiresAt    *time.Time
	SendingStartedAt *time.Time
	SentAt           *time.Time
	DLQAt            *time.Time
	QuarantinedAt    *time.Time
	CancelledAt      *time.Time
	Error            string
	ErrorCode        string
	EnqueuedAt       time.Time
	CreatedAt        time.Time
	UpdatedAt        time.Time
}

type EventRecord struct {
	ID                   int64
	EventKey             string
	PayloadHash          string
	AlarmType            domain.AlarmType
	ChannelID            string
	StreamID             string
	Category             string
	PayloadSchemaVersion int
	Payload              []byte
	CreatedAt            time.Time
	UpdatedAt            time.Time
}

// PublishBatchInput의 delivery는 항상 pending으로 저장한다.
type PublishBatchInput struct {
	Envelopes []domain.AlarmQueueEnvelope
}

// PublishOutcome은 commit으로 확인한 입력별 발행 결과다.
type PublishOutcome string

const (
	PublishInserted          PublishOutcome = "inserted"
	PublishDuplicateActive   PublishOutcome = "duplicate_active"
	PublishDuplicateSent     PublishOutcome = "duplicate_sent"
	PublishRejectedCollision PublishOutcome = "rejected_collision"
	PublishRejectedTerminal  PublishOutcome = "rejected_terminal"
)

// PublishReceipt의 Ordinal은 호출 입력의 위치이며 DedupeKey는 방별 영속 식별자다.
// Receipt가 없는 입력은 수용 여부가 확인되지 않았으므로 성공으로 표시하지 않는다.
type PublishReceipt struct {
	Ordinal   int
	DedupeKey string
	Outcome   PublishOutcome
	Status    Status
}

// Accepted는 pending 수용 또는 이미 성공한 동일 delivery만 인정한다.
func (r PublishReceipt) Accepted() bool {
	return r.Outcome == PublishInserted || r.Outcome == PublishDuplicateActive || r.Outcome == PublishDuplicateSent
}

type PublishBatchResult struct {
	Receipts            []PublishReceipt
	RequestedEvents     int
	InsertedEvents      int
	DuplicateEvents     int
	HashConflictEvents  int
	RequestedDeliveries int
	ProcessedDeliveries int
	InsertedDeliveries  int
	DuplicateDeliveries int
	TerminalDuplicates  int
}

func processedPublishBatchResult(result *PublishBatchResult) PublishBatchResult {
	result.ProcessedDeliveries = len(result.Receipts)
	return *result
}

// FailureUpdate의 AttemptCount는 기대 next 값이다. SQL CAS
// (input.attempt_count = attempt_count + 1)와 일치하지 않는 행은 미적용으로 남는다.
// NextAttemptAt은 TargetStatus가 StatusRetry일 때만 기록된다.
type FailureUpdate struct {
	ID            int64     `json:"id"`
	AttemptCount  int       `json:"attempt_count"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Error         string    `json:"error"`
	ErrorCode     string    `json:"error_code"`
	TargetStatus  Status    `json:"target_status"`
}

type TerminalUpdate struct {
	ID        int64  `json:"id"`
	Error     string `json:"error"`
	ErrorCode string `json:"error_code"`
}

type Writer interface {
	InsertPending(ctx context.Context, envelope *domain.AlarmQueueEnvelope) (*Record, InsertResult, error)
	InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error)
}

type LeaseManager interface {
	ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error)
	ReleaseLeased(ctx context.Context, ids []int64, workerID string) error
	RecoverExpiredLeased(ctx context.Context, limit int) (int, error)
}

type EventLoader interface {
	LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error)
}

type DeliveryProgressWriter interface {
	MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error
	MarkSent(ctx context.Context, ids []int64, workerID string) error
	RouteFailures(ctx context.Context, updates []FailureUpdate, workerID string) error
	RouteSendingFailures(ctx context.Context, updates []FailureUpdate, workerID string) error
	RequeuePreSend(ctx context.Context, updates []FailureUpdate, workerID string) error
}

type TerminalWriter interface {
	MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error
	Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error
	QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error)
}

type Repository interface {
	Writer
	LeaseManager
	EventLoader
	DeliveryProgressWriter
	TerminalWriter
}
