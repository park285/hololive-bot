package telemetry

import (
	"time"

	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/deliverysql"
	"github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type postDeliveryTimelineScanRow struct {
	OutboxID                   int64                                    `db:"outbox_id"`
	OutboxKind                 domain.OutboxKind                        `db:"outbox_kind"`
	AlarmType                  domain.AlarmType                         `db:"alarm_type"`
	ChannelID                  string                                   `db:"channel_id"`
	PostID                     string                                   `db:"post_id"`
	ContentID                  string                                   `db:"content_id"`
	ActualPublishedAt          *time.Time                               `db:"actual_published_at"`
	DetectedAt                 *time.Time                               `db:"detected_at"`
	QueueEnqueuedAt            *time.Time                               `db:"queue_enqueued_at"`
	FirstAttemptStartedAt      *time.Time                               `db:"first_attempt_started_at"`
	LastAttemptStartedAt       *time.Time                               `db:"last_attempt_started_at"`
	FirstAttemptFinishedAt     *time.Time                               `db:"first_attempt_finished_at"`
	LastAttemptFinishedAt      *time.Time                               `db:"last_attempt_finished_at"`
	AlarmSentAt                *time.Time                               `db:"alarm_sent_at"`
	FirstSuccessAt             *time.Time                               `db:"first_success_at"`
	LastSuccessAt              *time.Time                               `db:"last_success_at"`
	LastFailureAt              *time.Time                               `db:"last_failure_at"`
	NextRetryAt                *time.Time                               `db:"next_retry_at"`
	SuccessSendCount           int64                                    `db:"success_send_count"`
	FailedAttemptCount         int64                                    `db:"failed_attempt_count"`
	MaxAttemptOrdinal          int64                                    `db:"max_attempt_ordinal"`
	AlarmLatencyMillis         *int64                                   `db:"alarm_latency_millis"`
	AlarmLatencyExceeded       *bool                                    `db:"alarm_latency_exceeded"`
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
			ActualPublishedAt:          deliverysql.CloneUTCTimePtr(scanned[i].ActualPublishedAt),
			DetectedAt:                 deliverysql.CloneUTCTimePtr(scanned[i].DetectedAt),
			QueueEnqueuedAt:            deliverysql.CloneUTCTimePtr(scanned[i].QueueEnqueuedAt),
			FirstAttemptStartedAt:      deliverysql.CloneUTCTimePtr(scanned[i].FirstAttemptStartedAt),
			LastAttemptStartedAt:       deliverysql.CloneUTCTimePtr(scanned[i].LastAttemptStartedAt),
			FirstAttemptFinishedAt:     deliverysql.CloneUTCTimePtr(scanned[i].FirstAttemptFinishedAt),
			LastAttemptFinishedAt:      deliverysql.CloneUTCTimePtr(scanned[i].LastAttemptFinishedAt),
			AlarmSentAt:                deliverysql.CloneUTCTimePtr(scanned[i].AlarmSentAt),
			FirstSuccessAt:             deliverysql.CloneUTCTimePtr(scanned[i].FirstSuccessAt),
			LastSuccessAt:              deliverysql.CloneUTCTimePtr(scanned[i].LastSuccessAt),
			LastFailureAt:              deliverysql.CloneUTCTimePtr(scanned[i].LastFailureAt),
			NextRetryAt:                deliverysql.CloneUTCTimePtr(scanned[i].NextRetryAt),
			SuccessSendCount:           scanned[i].SuccessSendCount,
			FailedAttemptCount:         scanned[i].FailedAttemptCount,
			MaxAttemptOrdinal:          scanned[i].MaxAttemptOrdinal,
			AlarmLatencyMillis:         scanned[i].AlarmLatencyMillis,
			AlarmLatencyExceeded:       scanned[i].AlarmLatencyExceeded,
			StoredClassificationStatus: scanned[i].StoredClassificationStatus,
			StoredDelaySource:          scanned[i].StoredDelaySource,
			StoredInternalDelayCause:   scanned[i].StoredInternalDelayCause,
		}
		timeline.DeriveMetrics(&row)

		rows = append(rows, row)
	}

	return rows
}
