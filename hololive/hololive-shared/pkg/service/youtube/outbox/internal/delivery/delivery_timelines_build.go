package delivery

import "github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"

func buildPostDeliveryTimelinesFromScanRows(scanned []postDeliveryTimelineScanRow) []PostDeliveryTimeline {
	rows := make([]PostDeliveryTimeline, 0, len(scanned))
	for i := range scanned {
		row := PostDeliveryTimeline{
			OutboxID:                   scanned[i].OutboxID,
			OutboxKind:                 scanned[i].OutboxKind,
			AlarmType:                  scanned[i].AlarmType,
			ChannelID:                  scanned[i].ChannelID,
			PostID:                     scanned[i].PostID,
			ContentID:                  scanned[i].ContentID,
			ActualPublishedAt:          scanned[i].ActualPublishedAt.Ptr(),
			DetectedAt:                 scanned[i].DetectedAt.Ptr(),
			QueueEnqueuedAt:            scanned[i].QueueEnqueuedAt.Ptr(),
			FirstAttemptStartedAt:      scanned[i].FirstAttemptStartedAt.Ptr(),
			LastAttemptStartedAt:       scanned[i].LastAttemptStartedAt.Ptr(),
			FirstAttemptFinishedAt:     scanned[i].FirstAttemptFinishedAt.Ptr(),
			LastAttemptFinishedAt:      scanned[i].LastAttemptFinishedAt.Ptr(),
			AlarmSentAt:                scanned[i].AlarmSentAt.Ptr(),
			FirstSuccessAt:             scanned[i].FirstSuccessAt.Ptr(),
			LastSuccessAt:              scanned[i].LastSuccessAt.Ptr(),
			LastFailureAt:              scanned[i].LastFailureAt.Ptr(),
			NextRetryAt:                scanned[i].NextRetryAt.Ptr(),
			SuccessSendCount:           scanned[i].SuccessSendCount,
			FailedAttemptCount:         scanned[i].FailedAttemptCount,
			MaxAttemptOrdinal:          scanned[i].MaxAttemptOrdinal,
			AlarmLatencyMillis:         scanned[i].AlarmLatencyMillis,
			AlarmLatencyExceeded:       scanned[i].AlarmLatencyExceeded.Ptr(),
			StoredClassificationStatus: scanned[i].StoredClassificationStatus,
			StoredDelaySource:          scanned[i].StoredDelaySource,
			StoredInternalDelayCause:   scanned[i].StoredInternalDelayCause,
		}
		timeline.DeriveMetrics(&row)
		rows = append(rows, row)
	}
	return rows
}
