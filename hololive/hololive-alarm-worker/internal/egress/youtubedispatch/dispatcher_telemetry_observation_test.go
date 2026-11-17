package youtubedispatch

import (
	"bytes"
	jsonv2 "encoding/json/v2"
	"log/slog"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	telemetry "github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/telemetry"
	timeline "github.com/kapu/hololive-alarm-worker/internal/egress/youtubeoutbox/timeline"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildDeliveryAuditLogAttrsIncludesCommunityTimingFields(t *testing.T) {
	t.Parallel()

	actualPublishedAt := time.Date(2026, time.April, 10, 2, 15, 0, 0, time.UTC)
	detectedAt := actualPublishedAt.Add(35 * time.Second)
	alarmSentAt := detectedAt.Add(25 * time.Second)
	alarmLatencyMillis := int64(alarmSentAt.Sub(actualPublishedAt) / time.Millisecond)

	row := telemetry.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:         31,
		AttemptOrdinal:     1,
		OutboxID:           41,
		ChannelID:          "UC_community_observe",
		ContentID:          "community:post-observe",
		PostID:             "UgkxCommunityObserve",
		RoomID:             testRoomCommunity,
		AlarmType:          domain.AlarmTypeCommunity,
		ActualPublishedAt:  &actualPublishedAt,
		DetectedAt:         &detectedAt,
		AlarmSentAt:        &alarmSentAt,
		AlarmLatencyMillis: &alarmLatencyMillis,
		DedupeKey:          "youtube-notification:COMMUNITY_POST:community:post-observe",
		DeliveryPath:       telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:       deliveryModeGrouped,
		SendResult:         sendResultSuccess,
		EventAt:            alarmSentAt,
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(CommunityShortsDeliveryAuditMessage, buildDeliveryAuditLogAttrsWithClassification(&row, &timeline.PostLatencyClassificationResult{})...)

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
	row := telemetry.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     41,
		AttemptOrdinal: 2,
		OutboxID:       51,
		ChannelID:      "UC_latency_classification",
		ContentID:      "short-latency-classification",
		PostID:         "short-latency-classification",
		RoomID:         "room-latency-classification",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-latency-classification",
		DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:   deliveryModePerRoom,
		SendResult:     sendResultSuccess,
		EventAt:        time.Date(2026, time.April, 10, 2, 20, 0, 0, time.UTC),
	}
	classification := timeline.PostLatencyClassificationResult{
		Status:             timeline.PostLatencyClassificationStatusExceeded,
		ThresholdMillis:    timeline.PostLatencyExceededThresholdMillis,
		DelaySource:        timeline.PostDelaySourceInternalDelivery,
		InternalDelayCause: timeline.PostInternalDelayCauseRetryAccumulation,
		Evidence: []timeline.PostLatencyClassificationEvidence{{
			Key:      timeline.PostLatencyClassificationEvidenceKeyRetryAccumulation,
			Millis:   new(evidenceMillis),
			Selected: true,
		}},
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(CommunityShortsDeliveryAuditMessage, buildDeliveryAuditLogAttrsWithClassification(&row, &classification)...)

	var record struct {
		Classification struct {
			Status             string `json:"status"`
			ThresholdMillis    int64  `json:"threshold_millis"`
			DelaySource        string `json:"delay_source"`
			InternalDelayCause string `json:"internal_delay_cause"`
			ReasonCode         string `json:"reason_code"`
			Evidence           struct {
				Count int `json:"count"`
			} `json:"evidence"`
		} `json:"latency_classification"`
	}

	require.NoError(t, jsonv2.Unmarshal(buffer.Bytes(), &record))
	require.Equal(t, "exceeded", record.Classification.Status)
	require.Equal(t, int64(120000), record.Classification.ThresholdMillis)
	require.Equal(t, "internal_delivery", record.Classification.DelaySource)
	require.Equal(t, "retry_accumulation", record.Classification.InternalDelayCause)
	require.Equal(t, "retry_accumulation", record.Classification.ReasonCode)
	require.Equal(t, 1, record.Classification.Evidence.Count)
}

func TestAppendLatencyClassificationLogAttrAcceptsMissingClassification(t *testing.T) {
	t.Parallel()

	attrs := []any{slog.String("existing", "value")}

	got := appendLatencyClassificationLogAttr(attrs, nil)

	require.Len(t, got, 1)
}

func TestBuildDeliveryAuditLogAttrsWithClassificationIncludesExternalDelayReasonCode(t *testing.T) {
	t.Parallel()

	row := telemetry.YouTubeNotificationDeliveryTelemetry{
		DeliveryID:     51,
		AttemptOrdinal: 1,
		OutboxID:       61,
		ChannelID:      "UC_external_reason_code",
		ContentID:      "short-external-reason-code",
		PostID:         "short-external-reason-code",
		RoomID:         "room-external-reason-code",
		AlarmType:      domain.AlarmTypeShorts,
		DedupeKey:      "youtube-notification:NEW_SHORT:short-external-reason-code",
		DeliveryPath:   telemetry.CommunityShortsDeliveryPath,
		DeliveryMode:   deliveryModePerRoom,
		SendResult:     sendResultSuccess,
		EventAt:        time.Date(2026, time.April, 10, 2, 25, 0, 0, time.UTC),
	}
	classification := timeline.PostLatencyClassificationResult{
		Status:             timeline.PostLatencyClassificationStatusExceeded,
		ThresholdMillis:    timeline.PostLatencyExceededThresholdMillis,
		DelaySource:        timeline.PostDelaySourceExternalCollection,
		InternalDelayCause: timeline.PostInternalDelayCauseNone,
	}

	buffer := &bytes.Buffer{}
	logger := slog.New(slog.NewJSONHandler(buffer, &slog.HandlerOptions{Level: slog.LevelInfo}))
	logger.Info(CommunityShortsDeliveryAuditMessage, buildDeliveryAuditLogAttrsWithClassification(&row, &classification)...)

	logLine := buffer.String()
	require.Contains(t, logLine, `"delay_source":"external_collection"`)
	require.Contains(t, logLine, `"internal_delay_cause":"none"`)
	require.Contains(t, logLine, `"reason_code":"external_collection"`)
}
