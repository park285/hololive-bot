package observation

import (
	"context"
	"errors"
	"fmt"
	"strings"
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

	// 행은 기본 키 (kind, canonical_content_id) 하나로 찾는다.
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

// tracking upsert의 갱신 식은 상수 조각만으로 정해지므로 호출마다 조립하지 않고 초기화 때 한 번 만든다.
const (
	trackingFinalActualPublishedExpr = `COALESCE(youtube_content_alarm_tracking.actual_published_at, EXCLUDED.actual_published_at)`
	trackingFinalAlarmSentExpr       = `CASE
		        WHEN youtube_content_alarm_tracking.alarm_sent_at IS NULL THEN EXCLUDED.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at IS NULL THEN youtube_content_alarm_tracking.alarm_sent_at
		        WHEN EXCLUDED.alarm_sent_at < youtube_content_alarm_tracking.alarm_sent_at THEN EXCLUDED.alarm_sent_at
		        ELSE youtube_content_alarm_tracking.alarm_sent_at
		    END`
)

var (
	trackingLatencyMillisExpr   = buildLatencyMillisExpr(trackingFinalActualPublishedExpr, trackingFinalAlarmSentExpr)
	trackingLatencyExceededExpr = buildLatencyExceededExpr(trackingLatencyMillisExpr)
	trackingDeliveryStatusExpr  = buildDeliveryStatusExpr(trackingFinalAlarmSentExpr)
)

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
	query, args := buildTrackingUpsertQuery(normalized, now, trackingLatencyMillisExpr, trackingLatencyExceededExpr, trackingDeliveryStatusExpr)

	if _, err := dbx.ExecSQL(ctx, r.db, "upsert tracking batch: exec query", query, args...); err != nil {
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

func buildTrackingUpsertQuery(
	normalized []*domain.YouTubeContentAlarmTracking,
	now time.Time,
	latencyMillisExpr string,
	latencyExceededExpr string,
	deliveryStatusExpr string,
) (string, []any) {
	const columnsPerRow = 12

	args := make([]any, 0, len(normalized)*columnsPerRow)

	var sb strings.Builder

	sb.WriteString(buildTrackingUpsertQuerySQL)

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}

		writeRowPlaceholders(&sb, i, columnsPerRow)

		args = appendTrackingUpsertValues(args, record, now)
	}

	sb.WriteString(buildTrackingUpsertQuery2SQL)
	sb.WriteString(latencyMillisExpr)
	sb.WriteString(`,
		    alarm_latency_exceeded = `)
	sb.WriteString(latencyExceededExpr)
	sb.WriteString(`,
		    delivery_status = `)
	sb.WriteString(deliveryStatusExpr)
	sb.WriteString(`,
		    updated_at = EXCLUDED.updated_at
	`)

	return sb.String(), args
}

func appendTrackingUpsertValues(args []any, record *domain.YouTubeContentAlarmTracking, now time.Time) []any {
	return append(args,
		record.Kind,
		record.ContentID,
		record.CanonicalContentID,
		record.ChannelID,
		record.ActualPublishedAt,
		record.DetectedAt,
		record.AlarmSentAt,
		record.AlarmLatencyMillis,
		record.AlarmLatencyExceeded,
		record.DeliveryStatus,
		now,
		now,
	)
}
