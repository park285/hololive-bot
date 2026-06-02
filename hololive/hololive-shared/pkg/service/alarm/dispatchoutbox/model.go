package dispatchoutbox

import (
	"context"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type Status string

const (
	StatusShadowed    Status = "shadowed"
	StatusPending     Status = "pending"
	StatusLeased      Status = "leased"
	StatusRetry       Status = "retry"
	StatusSending     Status = "sending"
	StatusSent        Status = "sent"
	StatusDLQ         Status = "dlq"
	StatusQuarantined Status = "quarantined"
	StatusCancelled   Status = "cancelled"
)

type InsertResult string

const (
	Inserted          InsertResult = "inserted"
	DuplicateActive   InsertResult = "duplicate_active"
	DuplicateTerminal InsertResult = "duplicate_terminal"
	DuplicateShadowed InsertResult = "duplicate_shadowed"
	PromotedShadow    InsertResult = "promoted_shadow"
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
	Envelopes []domain.AlarmQueueEnvelope
	Status    Status
}

type PublishBatchResult struct {
	RequestedEvents       int
	InsertedEvents        int
	DuplicateEvents       int
	HashConflictEvents    int
	RequestedDeliveries   int
	ProcessedDeliveries   int
	InsertedDeliveries    int
	DuplicateDeliveries   int
	TerminalDuplicates    int
	ShadowedDuplicates    int
	PromotedShadowedCount int
}

func processedPublishBatchResult(result PublishBatchResult) PublishBatchResult {
	result.ProcessedDeliveries = result.RequestedDeliveries
	return result
}

type RetryUpdate struct {
	ID            int64     `json:"id"`
	AttemptCount  int       `json:"attempt_count"`
	NextAttemptAt time.Time `json:"next_attempt_at"`
	Error         string    `json:"error"`
}

type TerminalUpdate struct {
	ID    int64  `json:"id"`
	Error string `json:"error"`
}

type Writer interface {
	InsertShadowed(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, error)
	InsertPending(ctx context.Context, envelope domain.AlarmQueueEnvelope) (*Record, InsertResult, error)
	InsertBatch(ctx context.Context, input PublishBatchInput) (PublishBatchResult, error)
}

type Repository interface {
	Writer
	ClaimDue(ctx context.Context, workerID string, limit int, lease time.Duration) ([]*Record, error)
	LoadEventsByID(ctx context.Context, eventIDs []int64) (map[int64]EventRecord, error)
	MarkSending(ctx context.Context, ids []int64, workerID string, extendLease time.Duration) error
	MarkSent(ctx context.Context, ids []int64, workerID string) error
	ScheduleRetry(ctx context.Context, updates []RetryUpdate, workerID string) error
	ScheduleSendingRetry(ctx context.Context, updates []RetryUpdate, workerID string) error
	MoveToDLQ(ctx context.Context, updates []TerminalUpdate, workerID string) error
	Quarantine(ctx context.Context, updates []TerminalUpdate, workerID string) error
	ReleaseLeased(ctx context.Context, ids []int64, workerID string) error
	RecoverExpiredLeased(ctx context.Context, limit int) (int, error)
	QuarantineStaleSending(ctx context.Context, olderThan time.Duration, limit int) (int, error)
}
