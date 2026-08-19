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

type ReplyOutboxManualReplay struct {
	OutboxID int64
	Actor    string
	Reason   string
}

func (r *ReplyOutboxRepository) ReplayManualReview(ctx context.Context, replay ReplyOutboxManualReplay) (string, error) {
	if err := ensurePool(r.pool); err != nil {
		return "", err
	}
	if replay.OutboxID <= 0 {
		return "", errors.Join(ErrInvalidArgument, errors.New("outbox id must be positive"))
	}
	actor, err := requireOperatorActor(replay.Actor)
	if err != nil {
		return "", err
	}
	reason, err := requireOperatorReason(replay.Reason)
	if err != nil {
		return "", err
	}
	var outcome string
	err = r.pool.QueryRow(ctx, replyOutboxReplayManualReviewSQL, replay.OutboxID, actor, reason).Scan(&outcome)
	if err != nil {
		return "", fmt.Errorf("replay manual-review reply outbox row %d: %w", replay.OutboxID, err)
	}
	return outcome, nil
}

func (r *ReplyOutboxRepository) ManualReviewStats(ctx context.Context) (ReplyOutboxManualReviewStats, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxManualReviewStats{}, err
	}
	var stats ReplyOutboxManualReviewStats
	var oldestSeconds float64
	err := r.pool.QueryRow(ctx, replyOutboxManualReviewStatsSQL).Scan(&stats.Backlog, &oldestSeconds)
	if err != nil {
		return ReplyOutboxManualReviewStats{}, fmt.Errorf("observe reply outbox manual review backlog: %w", err)
	}
	stats.OldestAge = time.Duration(oldestSeconds * float64(time.Second))
	return stats, nil
}

type ReplyOutboxReclaim struct {
	Requeued             int64
	AcceptedManualReview int64
	SafetyManualReview   int64
}

func (r *ReplyOutboxRepository) ReclaimExpired(ctx context.Context, batchSize int32) (ReplyOutboxReclaim, error) {
	if err := ensurePool(r.pool); err != nil {
		return ReplyOutboxReclaim{}, err
	}
	if batchSize <= 0 {
		return ReplyOutboxReclaim{}, errors.Join(ErrInvalidArgument, errors.New("batch size must be positive"))
	}

	var reclaim ReplyOutboxReclaim
	replayHorizonMS, err := leaseMilliseconds(r.automaticReplayHorizon)
	if err != nil {
		return ReplyOutboxReclaim{}, err
	}
	err = r.pool.QueryRow(ctx, replyOutboxReclaimExpiredSQL, batchSize, r.maxAttempts, replayHorizonMS).
		Scan(&reclaim.Requeued, &reclaim.AcceptedManualReview, &reclaim.SafetyManualReview)
	if err != nil {
		return ReplyOutboxReclaim{}, fmt.Errorf("reclaim expired reply outbox leases: %w", err)
	}

	return reclaim, nil
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
		return ReplyOutboxEntry{}, err
	}
	clientRequestID, err := requireClientRequestID(entry.ClientRequestID)
	if err != nil {
		return ReplyOutboxEntry{}, err
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
