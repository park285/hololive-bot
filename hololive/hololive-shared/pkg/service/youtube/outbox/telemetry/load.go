package telemetry

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/deliverysql"
)

func (r *Repository) loadTrackingSnapshots(
	ctx context.Context,
	identities map[deliveryTelemetryIdentity]struct{},
) (map[deliveryTelemetryIdentity]deliveryTelemetryTrackingSnapshot, error) {
	kinds := make([]string, 0, len(identities))
	contentIDs := make([]string, 0, len(identities))

	for identity := range identities {
		kinds = append(kinds, string(identity.kind))
		contentIDs = append(contentIDs, identity.contentID)
	}

	var trackingRows []domain.YouTubeContentAlarmTracking

	if err := dbx.SelectSQL(ctx, r.db, &trackingRows, "enrich delivery telemetry context: load tracking rows", mustSQL("load_0032_01.sql"), kinds, contentIDs); err != nil {
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
