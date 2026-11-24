package observation

import (
	"context"
	"errors"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *identityRepository) FindByIdentity(ctx context.Context, kind domain.OutboxKind, contentID string) (*domain.YouTubeContentAlarmTracking, error) {
	if r == nil || r.db == nil {
		return nil, errors.New("find tracking by identity: db is nil")
	}

	normalizedKind, normalizedContentID, err := normalizeIdentity(kind, contentID)
	if err != nil {
		return nil, fmt.Errorf("find tracking by identity: %w", err)
	}

	canonicalContentID, err := canonicalTrackingIdentity(normalizedKind, normalizedContentID)
	if err != nil {
		return nil, fmt.Errorf("find tracking by identity: canonical identity: %w", err)
	}

	var records []domain.YouTubeContentAlarmTracking

	if err := dbx.SelectSQL(ctx, r.db, &records, "find tracking by identity: query row",
		findByIdentitySQL, normalizedKind, canonicalContentID); err != nil {
		return nil, fmt.Errorf("select SQL: %w", err)
	}

	if len(records) == 0 {
		//nolint:nilnil // 미존재는 오류가 아니며 alarm-worker 호출자 3곳이 tracking == nil로 분기한다(alarm_state_repository.go와 같은 계약). sentinel 오류로 바꾸려면 호출자를 함께 고쳐야 한다.
		return nil, nil
	}

	return &records[0], nil
}

func (r *identityRepository) UpsertBatch(ctx context.Context, records []*domain.YouTubeContentAlarmTracking) error {
	if len(records) == 0 {
		return nil
	}

	if r == nil || r.db == nil {
		return errors.New("upsert tracking batch: db is nil")
	}

	normalized, err := normalizeTrackingBatchRecords(records)
	if err != nil {
		return fmt.Errorf("normalize tracking batch records: %w", err)
	}

	now := timestamp.Normalize(time.Now())
	if _, err := dbx.ExecSQL(ctx, r.db, "upsert tracking batch: exec query", upsertTrackingBatchSQL, trackingUpsertArgs(normalized, now)...); err != nil {
		return fmt.Errorf("exec SQL: %w", err)
	}

	return nil
}

func normalizeTrackingBatchRecords(records []*domain.YouTubeContentAlarmTracking) ([]*domain.YouTubeContentAlarmTracking, error) {
	return normalizeBatchRecords(records, "upsert tracking batch", normalizeRecord,
		func(record *domain.YouTubeContentAlarmTracking) string {
			return trackingCanonicalKey(record.Kind, record.CanonicalContentID)
		}, mergeNormalizedTrackingRecord)
}

func trackingUpsertArgs(normalized []*domain.YouTubeContentAlarmTracking, now time.Time) []any {
	size := len(normalized)
	kinds := make([]string, size)
	contentIDs := make([]string, size)
	canonicalContentIDs := make([]string, size)
	channelIDs := make([]string, size)
	actualPublishedAts := make([]*time.Time, size)
	detectedAts := make([]time.Time, size)
	alarmSentAts := make([]*time.Time, size)
	latencyMillis := make([]*int64, size)
	latencyExceeded := make([]*bool, size)
	deliveryStatuses := make([]string, size)

	for i, record := range normalized {
		kinds[i] = string(record.Kind)
		contentIDs[i] = record.ContentID
		canonicalContentIDs[i] = record.CanonicalContentID
		channelIDs[i] = record.ChannelID
		actualPublishedAts[i] = record.ActualPublishedAt
		detectedAts[i] = record.DetectedAt
		alarmSentAts[i] = record.AlarmSentAt
		latencyMillis[i] = record.AlarmLatencyMillis
		latencyExceeded[i] = record.AlarmLatencyExceeded
		deliveryStatuses[i] = string(record.DeliveryStatus)
	}

	return []any{
		kinds, contentIDs, canonicalContentIDs, channelIDs, actualPublishedAts, detectedAts, alarmSentAts,
		latencyMillis, latencyExceeded, deliveryStatuses, now, alarmLatencyExceededThresholdMillis,
	}
}
