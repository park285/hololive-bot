package dispatchoutbox

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Status string

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

// DeliveryClaim은 소유자 전이의 대상 delivery와 호출자가 claim에서 관측한 attempt_count다. 모든 소유자 전이는 행의
// (status, locked_by, attempt_count)를 이 claim과 맞춘다. 행의 attempt_count는 claim·반환·만료 회수에서 바뀌지 않으므로
// claim마다 바뀌는 토큰은 locked_by다. 같은 worker ID가 같은 attempt의 행을 다시 claim하는 경우는 runner가 한 배치의
// 정산을 끝낸 뒤에만 다음 claim을 하는 직렬 루프라서 생기지 않는다. 이 runner를 병렬화하면 claim별 토큰이 필요하다.
type DeliveryClaim struct {
	ID           int64 `json:"id"`
	AttemptCount int   `json:"attempt_count"`
}

// FailureUpdate의 AttemptCount는 전이 뒤 attempt_count다. 실패 정산은 claim attempt + 1, 같은 attempt 재대기는 claim
// attempt와 같아야 하며 SQL fence가 이 관계로 claim 세대를 맞춘다. RetryAfter는 TargetStatus가 StatusRetry일 때만
// 쓰이고 SQL이 잠금 뒤 채취한 clock에 더해 next_attempt_at을 정한다.
type FailureUpdate struct {
	ID           int64         `json:"id"`
	AttemptCount int           `json:"attempt_count"`
	RetryAfter   time.Duration `json:"-"`
	Error        string        `json:"error"`
	ErrorCode    string        `json:"error_code"`
	TargetStatus Status        `json:"target_status"`
}

// TerminalUpdate의 AttemptCount는 claim 당시 attempt_count이며 종료 전이는 attempt를 바꾸지 않는다.
type TerminalUpdate struct {
	ID           int64  `json:"id"`
	AttemptCount int    `json:"attempt_count"`
	Error        string `json:"error"`
	ErrorCode    string `json:"error_code"`
}

type Writer interface {
	InsertPending(ctx context.Context, envelope *domain.AlarmQueueEnvelope) (*Record, InsertResult, error)
	InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error)
}

type LeaseManager interface {
	ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error)
	ReleaseLeased(ctx context.Context, claims []DeliveryClaim, workerID string) error
	RecoverExpiredLeased(ctx context.Context, limit int) (int, error)
}

type EventLoader interface {
	LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error)
}

// DeliveryProgressWriter의 전이는 fence가 맞지 않은 대상을 바꾸지 않고 *sendfence.MissError로 보고한다.
type DeliveryProgressWriter interface {
	BeginSendRequest(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, expectedID string, lease time.Duration) error
	MarkSent(ctx context.Context, unitID int64, claims []DeliveryClaim, workerID, irisRequestID string, firstCheck time.Duration) error
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
