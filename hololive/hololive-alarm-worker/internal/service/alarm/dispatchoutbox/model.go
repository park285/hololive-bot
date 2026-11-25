package dispatchoutbox

import (
	"context"
	"time"

	contractsalarm "github.com/kapu/hololive-shared/pkg/contracts/alarm"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type Status string

const (
	StatusPending     Status = contractsalarm.DispatchStatusPending
	StatusLeased      Status = contractsalarm.DispatchStatusLeased
	StatusRetry       Status = contractsalarm.DispatchStatusRetry
	StatusSending     Status = contractsalarm.DispatchStatusSending
	StatusSent        Status = contractsalarm.DispatchStatusSent
	StatusDLQ         Status = contractsalarm.DispatchStatusDLQ
	StatusQuarantined Status = contractsalarm.DispatchStatusQuarantined
	StatusCancelled   Status = contractsalarm.DispatchStatusCancelled
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
	ClaimKeys        []string
	DeliveryContext  []byte
	DispatchGroupKey string
	SendUnitID       int64
	ClientRequestID  string
	RequestUnpinned  bool
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

type PublishBatchInput struct {
	Envelopes []AlarmQueueEnvelope
}

type PublishOutcome string

const (
	PublishInserted          PublishOutcome = "inserted"
	PublishDuplicateActive   PublishOutcome = "duplicate_active"
	PublishDuplicateSent     PublishOutcome = "duplicate_sent"
	PublishRejectedCollision PublishOutcome = "rejected_collision"
	PublishRejectedTerminal  PublishOutcome = "rejected_terminal"
)

type PublishReceipt struct {
	Ordinal   int
	DedupeKey string
	Outcome   PublishOutcome
	Status    Status
}

func (r PublishReceipt) Accepted() bool {
	return r.Outcome == PublishInserted || r.Outcome == PublishDuplicateActive || r.Outcome == PublishDuplicateSent
}

type PublishBatchResult struct {
	Receipts             []PublishReceipt
	RequestedEvents      int
	InsertedEvents       int
	DuplicateEvents      int
	HashConflictEvents   int
	RequestedDeliveries  int
	ProcessedDeliveries  int
	InsertedDeliveries   int
	PausedRoomDeliveries int
	DuplicateDeliveries  int
	TerminalDuplicates   int
}

func processedPublishBatchResult(result *PublishBatchResult) PublishBatchResult {
	result.ProcessedDeliveries = len(result.Receipts)
	return *result
}

type DeliveryClaim struct {
	ID           int64 `json:"id"`
	AttemptCount int   `json:"attempt_count"`
}

type FailureUpdate struct {
	ID           int64         `json:"id"`
	AttemptCount int           `json:"attempt_count"`
	RetryAfter   time.Duration `json:"-"`
	Error        string        `json:"error"`
	ErrorCode    string        `json:"error_code"`
	TargetStatus Status        `json:"target_status"`
}

type TerminalUpdate struct {
	ID           int64  `json:"id"`
	AttemptCount int    `json:"attempt_count"`
	Error        string `json:"error"`
	ErrorCode    string `json:"error_code"`
}

type Writer interface {
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
