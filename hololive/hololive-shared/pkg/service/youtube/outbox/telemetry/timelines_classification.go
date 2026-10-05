package telemetry

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/timeline"
)

func (r *Repository) PersistPostLatencyClassificationsByIdentities(
	ctx context.Context,
	identities []timeline.PostTrackingIdentity,
) error {
	if r == nil || r.db == nil {
		return errors.New("persist post latency classifications by identities: db is nil")
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

// persistPostLatencyClassifications는 분류 결과를 한 UPDATE 문장으로 반영한다. 호출자가 transaction Querier를
// 넘기면 그 transaction 안에서 실행되어, 같은 transaction이 앞서 쓴 tracking·telemetry 행을 보고 함께 rollback된다.
// 같은 (kind, content_id)가 여러 행이면 처음 행의 분류만 쓴다.
func (r *Repository) persistPostLatencyClassifications(ctx context.Context, rows []timeline.PostDeliveryTimeline) error {
	batch := newPostLatencyClassificationBatch(len(rows))
	seen := make(map[string]struct{}, len(rows))

	for i := range rows {
		contentID, ok := markPostLatencyClassificationRowSeen(&rows[i], seen)
		if !ok {
			continue
		}

		batch.add(&rows[i], contentID)
	}

	if len(batch.kinds) == 0 {
		return nil
	}

	if _, err := r.db.Exec(ctx, mustSQL("timelines_classification_0105_01.sql"),
		batch.kinds, batch.contentIDs, batch.statuses, batch.delaySources, batch.internalDelayCauses, time.Now().UTC(),
	); err != nil {
		return fmt.Errorf("update post latency classifications (%d identities): %w", len(batch.kinds), err)
	}

	return nil
}

// postLatencyClassificationBatch는 UPDATE ... FROM unnest에 넘길 같은 길이의 열 배열이다.
type postLatencyClassificationBatch struct {
	kinds               []string
	contentIDs          []string
	statuses            []string
	delaySources        []string
	internalDelayCauses []string
}

func newPostLatencyClassificationBatch(capacity int) postLatencyClassificationBatch {
	return postLatencyClassificationBatch{
		kinds:               make([]string, 0, capacity),
		contentIDs:          make([]string, 0, capacity),
		statuses:            make([]string, 0, capacity),
		delaySources:        make([]string, 0, capacity),
		internalDelayCauses: make([]string, 0, capacity),
	}
}

func (b *postLatencyClassificationBatch) add(row *timeline.PostDeliveryTimeline, contentID string) {
	status, delaySource, internalDelayCause := normalizedPostLatencyClassificationPersistenceValues(row)

	b.kinds = append(b.kinds, string(row.OutboxKind))
	b.contentIDs = append(b.contentIDs, contentID)
	b.statuses = append(b.statuses, string(status))
	b.delaySources = append(b.delaySources, string(delaySource))
	b.internalDelayCauses = append(b.internalDelayCauses, string(internalDelayCause))
}

func markPostLatencyClassificationRowSeen(row *timeline.PostDeliveryTimeline, seen map[string]struct{}) (string, bool) {
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

func normalizedPostLatencyClassificationPersistenceValues(
	row *timeline.PostDeliveryTimeline,
) (timeline.PostLatencyClassificationStatus, timeline.PostDelaySource, timeline.PostInternalDelayCause) {
	status := row.LatencyClassification.Status
	if status == "" {
		status = timeline.PostLatencyClassificationStatusInsufficientEvidence
	}

	delaySource := row.DelaySource
	if delaySource == "" {
		delaySource = timeline.PostDelaySourceNone
	}

	internalDelayCause := row.InternalDelayCause
	if internalDelayCause == "" {
		internalDelayCause = timeline.PostInternalDelayCauseNone
	}

	return status, delaySource, internalDelayCause
}
