package delivery

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func (r *DeliveryTelemetryRepository) PersistPostLatencyClassificationsByOutboxIDs(ctx context.Context, outboxIDs []int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: db is nil")
	}

	uniqueIDs := uniqueInt64s(outboxIDs)
	if len(uniqueIDs) == 0 {
		return nil
	}

	rows, err := r.ListPostDeliveryTimelinesByOutboxIDs(ctx, uniqueIDs)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: %w", err)
	}
	if err := r.persistPostLatencyClassifications(ctx, rows); err != nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: %w", err)
	}
	return nil
}

func (r *DeliveryTelemetryRepository) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []PostTrackingIdentity,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by identities: db is nil")
	}

	normalized, err := normalizePostTrackingIdentities(identities)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	if len(normalized) == 0 {
		return nil
	}

	rows, err := r.ListPostDeliveryTimelinesByTrackingIdentities(ctx, normalized)
	if err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	if err := r.persistPostLatencyClassifications(ctx, rows); err != nil {
		return fmt.Errorf("persist post latency classifications by identities: %w", err)
	}
	return nil
}

func (r *DeliveryTelemetryRepository) persistPostLatencyClassifications(ctx context.Context, rows []PostDeliveryTimeline) error {
	if len(rows) == 0 {
		return nil
	}

	updatedAt := time.Now().UTC()
	seen := make(map[string]struct{}, len(rows))
	for i := range rows {
		contentID, ok := markPostLatencyClassificationRowSeen(rows[i], seen)
		if !ok {
			continue
		}
		if err := r.updatePostLatencyClassification(ctx, rows[i], contentID, updatedAt); err != nil {
			return fmt.Errorf("update persisted latency classification: kind=%s content_id=%s: %w", rows[i].OutboxKind, contentID, err)
		}
	}

	return nil
}

func markPostLatencyClassificationRowSeen(row PostDeliveryTimeline, seen map[string]struct{}) (string, bool) {
	if !isCommunityShortsDeliveryAuditKind(row.OutboxKind) {
		return "", false
	}

	contentID := strings.TrimSpace(row.ContentID)
	if contentID == "" {
		return "", false
	}

	key := postTrackingIdentityKey(row.OutboxKind, contentID)
	if _, ok := seen[key]; ok {
		return "", false
	}
	seen[key] = struct{}{}

	return contentID, true
}

func (r *DeliveryTelemetryRepository) updatePostLatencyClassification(
	ctx context.Context,
	row PostDeliveryTimeline,
	contentID string,
	updatedAt time.Time,
) error {
	status, delaySource, internalDelayCause := normalizedPostLatencyClassificationPersistenceValues(row)
	return r.db.WithContext(ctx).
		Model(&domain.YouTubeContentAlarmTracking{}).
		Where("kind = ? AND content_id = ?", row.OutboxKind, contentID).
		Updates(map[string]any{
			"latency_classification_status": string(status),
			"delay_source":                  string(delaySource),
			"internal_delay_cause":          string(internalDelayCause),
			"updated_at":                    updatedAt,
		}).Error
}

func normalizedPostLatencyClassificationPersistenceValues(
	row PostDeliveryTimeline,
) (PostLatencyClassificationStatus, PostDelaySource, PostInternalDelayCause) {
	status := row.LatencyClassification.Status
	if status == "" {
		status = PostLatencyClassificationStatusInsufficientEvidence
	}
	delaySource := row.DelaySource
	if delaySource == "" {
		delaySource = PostDelaySourceNone
	}
	internalDelayCause := row.InternalDelayCause
	if internalDelayCause == "" {
		internalDelayCause = PostInternalDelayCauseNone
	}

	return status, delaySource, internalDelayCause
}
