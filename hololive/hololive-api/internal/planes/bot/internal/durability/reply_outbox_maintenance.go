package durability

import (
	"context"
	"errors"
	"fmt"
	"math"
	"slices"
	"time"
)

type ReplyOutboxManualReviewStats struct {
	Backlog   int64
	OldestAge time.Duration
}

func (r *ReplyOutboxRepository) ManualReviewStats(ctx context.Context) (ReplyOutboxManualReviewStats, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxManualReviewStats{}, err
	}

	var (
		stats         ReplyOutboxManualReviewStats
		oldestSeconds float64
	)

	err := r.pool.QueryRow(ctx, replyOutboxManualReviewStatsSQL).Scan(&stats.Backlog, &oldestSeconds)
	if err != nil {
		return ReplyOutboxManualReviewStats{}, fmt.Errorf("observe reply outbox manual review backlog: %w", err)
	}

	stats.OldestAge = time.Duration(oldestSeconds * float64(time.Second))

	return stats, nil
}

type ReplyOutboxReclaim struct {
	Requeued           int64
	SafetyManualReview int64
}

func normalizeReplyOutboxEntry(entry *ReplyOutboxEntry) (ReplyOutboxEntry, error) {
	messageID, err := requireMessageIdentity(entry.MessageID)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}

	phase, err := requireBoundedIdentity("phase", entry.Phase, phaseRuneLimit)
	if err != nil {
		return ReplyOutboxEntry{}, err
	}

	roomID, err := requireRoomID(entry.RoomID)
	if err != nil {
		return ReplyOutboxEntry{}, fmt.Errorf("require room ID: %w", err)
	}

	clientRequestID, err := requireClientRequestID(entry.ClientRequestID)
	if err != nil {
		return ReplyOutboxEntry{}, fmt.Errorf("require client request ID: %w", err)
	}

	if entry.Ordinal > math.MaxInt64 {
		return ReplyOutboxEntry{}, errors.Join(ErrInvalidArgument,
			fmt.Errorf("ordinal %d exceeds the BIGINT ledger domain", entry.Ordinal))
	}

	if len(entry.Payload) == 0 {
		return ReplyOutboxEntry{}, errors.Join(ErrInvalidArgument, errors.New("payload must not be empty"))
	}

	return ReplyOutboxEntry{
		MessageID:       messageID,
		Phase:           phase,
		Ordinal:         entry.Ordinal,
		RoomID:          roomID,
		Payload:         slices.Clone(entry.Payload),
		ClientRequestID: clientRequestID,
	}, nil
}
