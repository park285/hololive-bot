package outbox

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type deliveryTelemetryBackfillCandidate struct {
	DeliveryID        int64
	OutboxID          int64
	RoomID            string
	Status            domain.OutboxStatus
	AttemptCount      int
	DeliveryError     string
	DeliverySentAt    *time.Time
	DeliveryLockedAt  *time.Time
	DeliveryCreatedAt time.Time
	Kind              domain.OutboxKind
	ChannelID         string
	ContentID         string
	Payload           string
}

func (r *DeliveryTelemetryRepository) BackfillFromDelivery(ctx context.Context, limit int, since time.Time) (int, error) {
	if limit <= 0 {
		return 0, nil
	}

	candidates, err := r.loadBackfillCandidates(ctx, limit, since)
	if err != nil {
		return 0, err
	}
	events := buildBackfillEvents(candidates)
	if len(events) == 0 {
		return 0, nil
	}
	if err := r.Enqueue(ctx, events); err != nil {
		return 0, err
	}
	if err := r.PersistPostLatencyClassificationsByOutboxIDs(ctx, collectTelemetryOutboxIDs(events)); err != nil {
		return 0, fmt.Errorf("persist backfilled post latency classifications: %w", err)
	}

	return len(events), nil
}

func (r *DeliveryTelemetryRepository) loadBackfillCandidates(
	ctx context.Context,
	limit int,
	since time.Time,
) ([]deliveryTelemetryBackfillCandidate, error) {
	var candidates []deliveryTelemetryBackfillCandidate
	query := r.db.WithContext(ctx).
		Table("youtube_notification_delivery AS d").
		Select(strings.Join([]string{
			"d.id AS delivery_id",
			"d.outbox_id AS outbox_id",
			"d.room_id AS room_id",
			"d.status AS status",
			"d.attempt_count AS attempt_count",
			"d.error AS delivery_error",
			"d.sent_at AS delivery_sent_at",
			"d.locked_at AS delivery_locked_at",
			"d.created_at AS delivery_created_at",
			"o.kind AS kind",
			"o.channel_id AS channel_id",
			"o.content_id AS content_id",
			"o.payload AS payload",
		}, ", ")).
		Joins("JOIN youtube_notification_outbox o ON o.id = d.outbox_id").
		Where("o.kind IN ?", []domain.OutboxKind{domain.OutboxKindNewShort, domain.OutboxKindCommunityPost}).
		Where(`
			(d.status = ? AND d.sent_at IS NOT NULL)
			OR (d.status IN (?, ?) AND d.attempt_count > 0 AND COALESCE(d.error, '') <> '')
		`, domain.OutboxStatusSent, domain.OutboxStatusPending, domain.OutboxStatusFailed)
	if !since.IsZero() {
		query = query.Where("COALESCE(d.sent_at, d.locked_at, d.created_at) >= ?", since.UTC())
	}
	query = query.Order("COALESCE(d.sent_at, d.locked_at, d.created_at) ASC").
		Limit(limit)
	if err := query.Scan(&candidates).Error; err != nil {
		return nil, fmt.Errorf("backfill delivery telemetry candidates: %w", err)
	}

	return candidates, nil
}

func buildBackfillEvents(candidates []deliveryTelemetryBackfillCandidate) []domain.YouTubeNotificationDeliveryTelemetry {
	events := make([]domain.YouTubeNotificationDeliveryTelemetry, 0, len(candidates))
	for i := range candidates {
		event, ok := buildBackfillEvent(candidates[i])
		if !ok {
			continue
		}
		events = append(events, *event)
	}

	return events
}

func buildBackfillEvent(candidate deliveryTelemetryBackfillCandidate) (*domain.YouTubeNotificationDeliveryTelemetry, bool) {
	attemptOrdinal, sendResult, failureReason := backfillAttemptMetadata(candidate)
	if attemptOrdinal <= 0 {
		return nil, false
	}

	eventAt := backfillCandidateEventAt(candidate)
	dedupeKey, dedupeErr := domain.BuildYouTubeNotificationDedupeKey(candidate.Kind, candidate.ContentID)
	if dedupeErr != nil {
		dedupeKey = dedupeKeyLogValue(domain.YouTubeNotificationOutbox{Kind: candidate.Kind, ContentID: candidate.ContentID})
	}
	attemptStartedAt := cloneUTCTimePtr(candidate.DeliveryLockedAt)
	attemptFinishedAt := eventAt

	return &domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:        candidate.DeliveryID,
		AttemptOrdinal:    attemptOrdinal,
		OutboxID:          candidate.OutboxID,
		ChannelID:         candidate.ChannelID,
		ContentID:         candidate.ContentID,
		PostID:            resolveTelemetryPostID(candidate.Kind, candidate.ContentID, candidate.Payload),
		RoomID:            candidate.RoomID,
		AlarmType:         candidate.Kind.ToAlarmType(),
		DedupeKey:         dedupeKey,
		DeliveryPath:      communityShortsDeliveryPath,
		DeliveryMode:      "recovered",
		SendResult:        sendResult,
		FailureReason:     truncateString(failureReason, 100),
		AttemptStartedAt:  attemptStartedAt,
		AttemptFinishedAt: &attemptFinishedAt,
		EventAt:           eventAt,
		NextAttemptAt:     time.Now().UTC(),
	}, true
}

func backfillAttemptMetadata(candidate deliveryTelemetryBackfillCandidate) (int, string, string) {
	attemptOrdinal := candidate.AttemptCount
	sendResult := "failure"
	failureReason := strings.TrimSpace(candidate.DeliveryError)
	if candidate.Status == domain.OutboxStatusSent {
		attemptOrdinal = candidate.AttemptCount + 1
		sendResult = "success"
		failureReason = ""
	}

	return attemptOrdinal, sendResult, failureReason
}

func backfillCandidateEventAt(candidate deliveryTelemetryBackfillCandidate) time.Time {
	eventAt := candidate.DeliveryCreatedAt.UTC()
	if candidate.DeliverySentAt != nil {
		return candidate.DeliverySentAt.UTC()
	}
	if candidate.DeliveryLockedAt != nil {
		return candidate.DeliveryLockedAt.UTC()
	}
	return eventAt
}
