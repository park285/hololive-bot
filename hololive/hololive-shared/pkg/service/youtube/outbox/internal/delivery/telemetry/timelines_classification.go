package telemetry

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

func (r *Repository) PersistPostLatencyClassificationsByOutboxIDs(ctx context.Context, outboxIDs []int64) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by outbox ids: db is nil")
	}

	uniqueIDs := deliverysql.UniqueInt64s(outboxIDs)
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

func (r *Repository) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []PostTrackingIdentity,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("persist post latency classifications by identities: db is nil")
	}

	normalized, err := timeline.NormalizePostTrackingIdentities(identities)
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

func (r *Repository) persistPostLatencyClassifications(ctx context.Context, rows []PostDeliveryTimeline) error {
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
	if !IsCommunityShortsDeliveryAuditKind(row.OutboxKind) {
		return "", false
	}

	contentID := strings.TrimSpace(row.ContentID)
	if contentID == "" {
		return "", false
	}

	key := timeline.PostTrackingIdentityKey(row.OutboxKind, contentID)
	if _, ok := seen[key]; ok {
		return "", false
	}
	seen[key] = struct{}{}

	return contentID, true
}

func (r *Repository) updatePostLatencyClassification(
	ctx context.Context,
	row PostDeliveryTimeline,
	contentID string,
	updatedAt time.Time,
) error {
	status, delaySource, internalDelayCause := normalizedPostLatencyClassificationPersistenceValues(row)
	_, err := r.db.Exec(ctx, `
		UPDATE youtube_content_alarm_tracking
		SET latency_classification_status = $1,
		    delay_source = $2,
		    internal_delay_cause = $3,
		    updated_at = $4
		WHERE kind = $5 AND content_id = $6
	`, string(status), string(delaySource), string(internalDelayCause), updatedAt, row.OutboxKind, contentID)
	return err
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
