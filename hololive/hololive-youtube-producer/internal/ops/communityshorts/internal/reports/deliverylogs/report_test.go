package deliverylogs

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuild(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 10, 12, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	observationEndedAt := observationStartedAt.Add(24 * time.Hour)
	communityPublishedAt := generatedAt.Add(-2 * time.Hour)
	communityDetectedAt := generatedAt.Add(-119 * time.Minute)
	communityFirstEventAt := generatedAt.Add(-118 * time.Minute)
	communitySecondEventAt := generatedAt.Add(-117 * time.Minute)
	communityAlarmLatencyMillis := int64(communityFirstEventAt.Sub(communityPublishedAt) / time.Millisecond)
	shortDetectedAt := generatedAt.Add(-30 * time.Minute)
	shortEventAt := generatedAt.Add(-29 * time.Minute)

	report := Build(
		Query{
			Mode:                        QueryModeObservation,
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
			WindowStart:                 &observationStartedAt,
			WindowEnd:                   &observationEndedAt,
			Limit:                       2,
			Truncated:                   true,
		},
		[]domain.YouTubeNotificationDeliveryTelemetry{
			{
				ID:                 1,
				DeliveryID:         101,
				AttemptOrdinal:     1,
				ChannelID:          "UC_COMMUNITY",
				ContentID:          "post-community",
				PostID:             "post-community",
				RoomID:             "room-community",
				AlarmType:          domain.AlarmTypeCommunity,
				ActualPublishedAt:  &communityPublishedAt,
				AlarmSentAt:        &communityFirstEventAt,
				AlarmLatencyMillis: &communityAlarmLatencyMillis,
				DetectedAt:         &communityDetectedAt,
				DeliveryPath:       "youtube_outbox_dispatcher",
				DeliveryMode:       "grouped",
				SendResult:         "success",
				ObservationStatus:  "matched",
				EventAt:            communityFirstEventAt,
			},
			{
				ID:                 2,
				DeliveryID:         102,
				AttemptOrdinal:     2,
				ChannelID:          "UC_COMMUNITY",
				ContentID:          "post-community",
				PostID:             "post-community",
				RoomID:             "room-community",
				AlarmType:          domain.AlarmTypeCommunity,
				ActualPublishedAt:  &communityPublishedAt,
				AlarmSentAt:        &communityFirstEventAt,
				AlarmLatencyMillis: &communityAlarmLatencyMillis,
				DetectedAt:         &communityDetectedAt,
				DeliveryPath:       "youtube_outbox_dispatcher",
				DeliveryMode:       "grouped",
				SendResult:         "failure",
				FailureReason:      "retry",
				ObservationStatus:  "matched",
				EventAt:            communitySecondEventAt,
			},
			{
				ID:                3,
				DeliveryID:        103,
				AttemptOrdinal:    1,
				ChannelID:         "UC_SHORT",
				ContentID:         "short-recent",
				RoomID:            "room-short",
				AlarmType:         domain.AlarmTypeShorts,
				AlarmSentAt:       &shortEventAt,
				DetectedAt:        &shortDetectedAt,
				DeliveryPath:      "youtube_outbox_dispatcher",
				DeliveryMode:      "grouped",
				SendResult:        "success",
				ObservationStatus: "matched",
				EventAt:           shortEventAt,
			},
		},
		generatedAt,
	)

	require.Equal(t, generatedAt, report.GeneratedAt)
	require.Equal(t, QueryModeObservation, report.Query.Mode)
	require.Equal(t, "youtube-producer", report.Query.ObservationRuntimeName)
	require.NotNil(t, report.Query.ObservationBigBangCutoverAt)
	require.Equal(t, cutoverAt, report.Query.ObservationBigBangCutoverAt.UTC())
	require.NotNil(t, report.Query.WindowStart)
	require.Equal(t, observationStartedAt, report.Query.WindowStart.UTC())
	require.NotNil(t, report.Query.WindowEnd)
	require.Equal(t, observationEndedAt, report.Query.WindowEnd.UTC())
	require.Equal(t, 2, report.Query.Limit)
	require.True(t, report.Query.Truncated)

	require.Equal(t, 3, report.Summary.LogCount)
	require.Equal(t, 2, report.Summary.SuccessLogCount)
	require.Equal(t, 1, report.Summary.FailureLogCount)
	require.Equal(t, 2, report.Summary.UniquePostCount)
	require.Equal(t, 2, report.Summary.UniqueRoomCount)
	require.Len(t, report.Rows, 3)

	require.Equal(t, int64(103), report.Rows[0].DeliveryID)
	require.Equal(t, "short-recent", report.Rows[0].ContentID)
	require.NotNil(t, report.Rows[0].AlarmSentAt)
	require.Equal(t, shortEventAt, report.Rows[0].AlarmSentAt.UTC())
	require.Nil(t, report.Rows[0].PublishToEventMillis)
	require.NotNil(t, report.Rows[0].DetectToEventMillis)
	require.Equal(t, int64(shortEventAt.Sub(shortDetectedAt)/time.Millisecond), *report.Rows[0].DetectToEventMillis)

	require.Equal(t, int64(101), report.Rows[1].DeliveryID)
	require.Equal(t, int64(102), report.Rows[2].DeliveryID)
	require.NotNil(t, report.Rows[1].AlarmLatencyMillis)
	require.Equal(t, communityAlarmLatencyMillis, *report.Rows[1].AlarmLatencyMillis)
	require.NotNil(t, report.Rows[1].PublishToEventMillis)
	require.Equal(t, int64(communityFirstEventAt.Sub(communityPublishedAt)/time.Millisecond), *report.Rows[1].PublishToEventMillis)
	require.NotNil(t, report.Rows[2].PublishToEventMillis)
	require.Equal(t, int64(communitySecondEventAt.Sub(communityPublishedAt)/time.Millisecond), *report.Rows[2].PublishToEventMillis)

	markdown := RenderMarkdown(report)
	require.Contains(t, markdown, "# YouTube Community/Shorts Delivery Logs Report")
	require.Contains(t, markdown, "mode: `observation_window`")
	require.Contains(t, markdown, "truncated=`true`")
	require.Contains(t, markdown, "alarm_sent_at")
	require.Contains(t, markdown, "alarm_latency_millis")
	require.Contains(t, markdown, "`post-community`")
	require.Contains(t, markdown, "`short-recent`")
	require.Contains(t, markdown, "`retry`")
}

func TestRenderMarkdown_EmptyRows(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	since := generatedAt.Add(-24 * time.Hour)

	report := Build(
		Query{
			Mode:        QueryModeRecent,
			WindowStart: &since,
			WindowEnd:   &generatedAt,
			Limit:       200,
		},
		nil,
		generatedAt,
	)

	require.Equal(t, QueryModeRecent, report.Query.Mode)
	require.Equal(t, 0, report.Summary.LogCount)
	require.Empty(t, report.Rows)

	markdown := RenderMarkdown(report)
	require.Contains(t, markdown, "mode: `recent_window`")
	require.Contains(t, markdown, "조회된 community/shorts 발송 로그가 없습니다.")
}

func TestRenderMarkdown_ObservationMetadata(t *testing.T) {
	t.Parallel()

	generatedAt := time.Date(2026, 4, 12, 1, 0, 0, 0, time.UTC)
	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	observationStartedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	observationEndedAt := observationStartedAt.Add(24 * time.Hour)

	report := Build(
		Query{
			Mode:                        QueryModeObservation,
			WindowStart:                 &observationStartedAt,
			WindowEnd:                   &observationEndedAt,
			ObservationRuntimeName:      "youtube-producer",
			ObservationBigBangCutoverAt: &cutoverAt,
			Limit:                       200,
		},
		nil,
		generatedAt,
	)

	markdown := RenderMarkdown(report)
	require.Contains(t, markdown, "observation runtime: `youtube-producer`, cutover: `2026-04-10T00:00:00Z`")
	require.Contains(t, markdown, "window: `2026-04-10T01:00:00Z` -> `2026-04-11T01:00:00Z`")
}
