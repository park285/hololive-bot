package dispatch

import (
	"bytes"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildDeliveryAuditLogAttrsIncludesObservationWindowFields(t *testing.T) {
	t.Parallel()

	actualPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := actualPublishedAt.Add(20 * time.Second)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	observationEndedAt := observationStartedAt.Add(24 * time.Hour)

	row := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:                  11,
		AttemptOrdinal:              1,
		OutboxID:                    22,
		ChannelID:                   "UC_observe",
		ContentID:                   "short-observe",
		PostID:                      "short-observe",
		RoomID:                      "room-observe",
		AlarmType:                   domain.AlarmTypeShorts,
		ActualPublishedAt:           &actualPublishedAt,
		DetectedAt:                  &detectedAt,
		AlarmSentAt:                 new(detectedAt.Add(time.Minute)),
		AlarmLatencyMillis:          new(int64(55 * time.Second / time.Millisecond)),
		ObservationStatus:           deliveryTelemetryObservationStatusMatched,
		ObservationRuntimeName:      "youtube-producer",
		ObservationBigBangCutoverAt: &cutoverAt,
		ObservationStartedAt:        &observationStartedAt,
		ObservationEndedAt:          &observationEndedAt,
		DedupeKey:                   "youtube-notification:NEW_SHORT:short-observe",
		DeliveryPath:                communityShortsDeliveryPath,
		DeliveryMode:                "per_room",
		SendResult:                  "success",
		EventAt:                     detectedAt.Add(time.Minute),
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(deliveryAuditLogMessage, buildDeliveryAuditLogAttrs(&row)...)

	logLine := buffer.String()
	require.Contains(t, logLine, `"actual_published_at":`)
	require.Contains(t, logLine, `"alarm_sent_at":`)
	require.Contains(t, logLine, `"alarm_latency_millis":80000`)
	require.Contains(t, logLine, `"alarm_latency_exceeded":false`)
	require.Contains(t, logLine, `"detected_at":`)
	require.Contains(t, logLine, `"observation_status":"matched"`)
	require.Contains(t, logLine, `"observation_runtime_name":"youtube-producer"`)
	require.Contains(t, logLine, `"observation_bigbang_cutover_at":`)
	require.Contains(t, logLine, `"observation_started_at":`)
	require.Contains(t, logLine, `"observation_ended_at":`)
}

func TestBuildDeliveryAuditLogAttrsIncludesCommunityTimingFields(t *testing.T) {
	t.Parallel()

	actualPublishedAt := time.Date(2026, 4, 10, 2, 15, 0, 0, time.UTC)
	detectedAt := actualPublishedAt.Add(35 * time.Second)
	alarmSentAt := detectedAt.Add(25 * time.Second)
	alarmLatencyMillis := int64(alarmSentAt.Sub(actualPublishedAt) / time.Millisecond)

	row := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:         31,
		AttemptOrdinal:     1,
		OutboxID:           41,
		ChannelID:          "UC_community_observe",
		ContentID:          "community:post-observe",
		PostID:             "UgkxCommunityObserve",
		RoomID:             "room-community",
		AlarmType:          domain.AlarmTypeCommunity,
		ActualPublishedAt:  &actualPublishedAt,
		DetectedAt:         &detectedAt,
		AlarmSentAt:        &alarmSentAt,
		AlarmLatencyMillis: &alarmLatencyMillis,
		ObservationStatus:  deliveryTelemetryObservationStatusMatched,
		DedupeKey:          "youtube-notification:COMMUNITY_POST:community:post-observe",
		DeliveryPath:       communityShortsDeliveryPath,
		DeliveryMode:       "grouped",
		SendResult:         "success",
		EventAt:            alarmSentAt,
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(deliveryAuditLogMessage, buildDeliveryAuditLogAttrs(&row)...)

	logLine := buffer.String()
	require.Contains(t, logLine, `"alarm_type":"COMMUNITY"`)
	require.Contains(t, logLine, `"post_id":"UgkxCommunityObserve"`)
	require.Contains(t, logLine, `"actual_published_at":`)
	require.Contains(t, logLine, `"alarm_sent_at":`)
	require.Contains(t, logLine, `"alarm_latency_millis":60000`)
	require.Contains(t, logLine, `"alarm_latency_exceeded":false`)
	require.Contains(t, logLine, `"send_result":"success"`)
}

func TestBuildDeliveryAuditLogAttrsWithClassificationIncludesLatencyClassification(t *testing.T) {
	t.Parallel()

	evidenceMillis := int64(125 * time.Second / time.Millisecond)
	row := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     41,
		AttemptOrdinal: 2,
		OutboxID:       51,
		ChannelID:      "UC_latency_classification",
		ContentID:      "short-latency-classification",
		PostID:         "short-latency-classification",
		RoomID:         "room-latency-classification",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-latency-classification",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Date(2026, 4, 10, 2, 20, 0, 0, time.UTC),
	}
	classification := PostLatencyClassificationResult{
		Status:             PostLatencyClassificationStatusExceeded,
		ThresholdMillis:    postLatencyExceededThresholdMillis,
		DelaySource:        PostDelaySourceInternalDelivery,
		InternalDelayCause: PostInternalDelayCauseRetryAccumulation,
		Evidence: []PostLatencyClassificationEvidence{{
			Key:      PostLatencyClassificationEvidenceKeyRetryAccumulation,
			Millis:   new(evidenceMillis),
			Selected: true,
		}},
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(deliveryAuditLogMessage, buildDeliveryAuditLogAttrsWithClassification(&row, &classification)...)

	logLine := buffer.String()
	require.Contains(t, logLine, `"latency_classification":{"status":"exceeded"`)
	require.Contains(t, logLine, `"threshold_millis":120000`)
	require.Contains(t, logLine, `"delay_source":"internal_delivery"`)
	require.Contains(t, logLine, `"internal_delay_cause":"retry_accumulation"`)
	require.Contains(t, logLine, `"reason_code":"retry_accumulation"`)
	require.Contains(t, logLine, `"evidence":[{`)
}

func TestBuildDeliveryAuditLogAttrsWithClassificationIncludesExternalDelayReasonCode(t *testing.T) {
	t.Parallel()

	row := domain.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     51,
		AttemptOrdinal: 1,
		OutboxID:       61,
		ChannelID:      "UC_external_reason_code",
		ContentID:      "short-external-reason-code",
		PostID:         "short-external-reason-code",
		RoomID:         "room-external-reason-code",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-external-reason-code",
		DeliveryPath:   communityShortsDeliveryPath,
		DeliveryMode:   "per_room",
		SendResult:     "success",
		EventAt:        time.Date(2026, 4, 10, 2, 25, 0, 0, time.UTC),
	}
	classification := PostLatencyClassificationResult{
		Status:             PostLatencyClassificationStatusExceeded,
		ThresholdMillis:    postLatencyExceededThresholdMillis,
		DelaySource:        PostDelaySourceExternalCollection,
		InternalDelayCause: PostInternalDelayCauseNone,
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(deliveryAuditLogMessage, buildDeliveryAuditLogAttrsWithClassification(&row, &classification)...)

	logLine := buffer.String()
	require.Contains(t, logLine, `"delay_source":"external_collection"`)
	require.Contains(t, logLine, `"internal_delay_cause":"none"`)
	require.Contains(t, logLine, `"reason_code":"external_collection"`)
}
