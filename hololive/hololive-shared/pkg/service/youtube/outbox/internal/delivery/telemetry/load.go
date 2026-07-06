package telemetry

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/deliverysql"
)

func (r *Repository) loadTrackingSnapshots(
	ctx context.Context,
	identities map[deliveryTelemetryIdentity]struct{},
) (map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot, error) {
	kinds := make([]domain.OutboxKind, 0, len(identities))
	contentIDs := make([]string, 0, len(identities))
	kindSeen := make(map[domain.OutboxKind]struct{}, len(identities))
	contentSeen := make(map[string]struct{}, len(identities))
	for identity := range identities {
		if _, ok := kindSeen[identity.kind]; !ok {
			kindSeen[identity.kind] = struct{}{}
			kinds = append(kinds, identity.kind)
		}
		if _, ok := contentSeen[identity.contentID]; !ok {
			contentSeen[identity.contentID] = struct{}{}
			contentIDs = append(contentIDs, identity.contentID)
		}
	}

	var trackingRows []domain.YouTubeContentAlarmTracking
	if err := deliverysql.SelectDeliverySQL(ctx, r.db, &trackingRows, "enrich delivery telemetry context: load tracking rows", mustSQL("load_0032_01.sql")+deliverysql.DeliveryInClause("kind", len(kinds))+`
		  AND `+deliverysql.DeliveryInClause("content_id", len(contentIDs))+`
	`, deliverysql.AppendDeliveryStringArgs(deliverysql.AppendDeliveryOutboxKindArgs(nil, kinds...), contentIDs)...); err != nil {
		return nil, fmt.Errorf("enrich delivery telemetry context: load tracking rows: %w", err)
	}

	snapshots := make(map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot, len(trackingRows))
	for i := range trackingRows {
		row := trackingRows[i]
		detectedAt := row.DetectedAt.UTC()
		snapshots[deliveryTelemetryIdentity{kind: row.Kind, contentID: strings.TrimSpace(row.ContentID)}] = deliveryTelemetryTrackingSnapshot{
			actualPublishedAt: deliverysql.CloneUTCTimePtr(row.ActualPublishedAt),
			detectedAt:        &detectedAt,
			alarmSentAt:       deliverysql.CloneUTCTimePtr(row.AlarmSentAt),
		}
	}

	return snapshots, nil
}
