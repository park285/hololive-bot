package telemetry

import (
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/outbox/internal/delivery/timeline"
)

type postDeliveryTimelineScanRow struct {
	OutboxID                   int64                                    `db:"outbox_id"`
	OutboxKind                 domain.OutboxKind                        `db:"outbox_kind"`
	AlarmType                  domain.AlarmType                         `db:"alarm_type"`
	ChannelID                  string                                   `db:"channel_id"`
	PostID                     string                                   `db:"post_id"`
	ContentID                  string                                   `db:"content_id"`
	ActualPublishedAt          scannableTime                            `db:"actual_published_at"`
	DetectedAt                 scannableTime                            `db:"detected_at"`
	QueueEnqueuedAt            scannableTime                            `db:"queue_enqueued_at"`
	FirstAttemptStartedAt      scannableTime                            `db:"first_attempt_started_at"`
	LastAttemptStartedAt       scannableTime                            `db:"last_attempt_started_at"`
	FirstAttemptFinishedAt     scannableTime                            `db:"first_attempt_finished_at"`
	LastAttemptFinishedAt      scannableTime                            `db:"last_attempt_finished_at"`
	AlarmSentAt                scannableTime                            `db:"alarm_sent_at"`
	FirstSuccessAt             scannableTime                            `db:"first_success_at"`
	LastSuccessAt              scannableTime                            `db:"last_success_at"`
	LastFailureAt              scannableTime                            `db:"last_failure_at"`
	NextRetryAt                scannableTime                            `db:"next_retry_at"`
	SuccessSendCount           int64                                    `db:"success_send_count"`
	FailedAttemptCount         int64                                    `db:"failed_attempt_count"`
	MaxAttemptOrdinal          int64                                    `db:"max_attempt_ordinal"`
	AlarmLatencyMillis         *int64                                   `db:"alarm_latency_millis"`
	AlarmLatencyExceeded       scannableBool                            `db:"alarm_latency_exceeded"`
	StoredClassificationStatus timeline.PostLatencyClassificationStatus `db:"latency_classification_status"`
	StoredDelaySource          timeline.PostDelaySource                 `db:"delay_source"`
	StoredInternalDelayCause   timeline.PostInternalDelayCause          `db:"internal_delay_cause"`
}

func buildPostDeliveryTimelinesFromScanRows(scanned []postDeliveryTimelineScanRow) []timeline.PostDeliveryTimeline {
	rows := make([]timeline.PostDeliveryTimeline, 0, len(scanned))
	for i := range scanned {
		row := timeline.PostDeliveryTimeline{
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
