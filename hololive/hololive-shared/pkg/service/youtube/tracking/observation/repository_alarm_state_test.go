package observation

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryRejectsUnsupportedKind(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	err := repository.Upsert(t.Context(), &domain.YouTubeContentAlarmTracking{
		Kind:       domain.OutboxKindNewVideo,
		ContentID:  "video-1",
		ChannelID:  "UC_VIDEO",
		DetectedAt: time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "unsupported tracking kind")
}

func TestRepositoryMarkAlarmSentBatchPreservesEarliestTimestamp(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 6, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, time.April, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, time.April, 10, 1, 9, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         testCommunityPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{Kind: domain.OutboxKindCommunityPost, ContentID: testCommunityPostID, AlarmSentAt: laterAlarmSentAt},
		{Kind: domain.OutboxKindCommunityPost, ContentID: testCommunityPostID, AlarmSentAt: firstAlarmSentAt},
	}))
	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{Kind: domain.OutboxKindCommunityPost, ContentID: testCommunityPostID, AlarmSentAt: laterAlarmSentAt},
	}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, testCommunityPostID)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, record.AlarmSentAt.UTC())
	require.NotNil(t, record.AlarmLatencyMillis)
	require.Equal(t, int64(3*time.Minute/time.Millisecond), *record.AlarmLatencyMillis)
	require.NotNil(t, record.AlarmLatencyExceeded)
	require.True(t, *record.AlarmLatencyExceeded)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchUpdatesLegacyRawShortRowFromCanonicalMark(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 6, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, time.April, 10, 1, 8, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         testShortContentID,
		ChannelID:         testShortChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   testShortCanonicalPostID,
		AlarmSentAt: alarmSentAt,
	}}))

	record, err := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, testShortContentID)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
}

func TestRepositoryMarkAlarmSentBatchRepairsMissingAlarmStateForAlreadySentTracking(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 6, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, time.April, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, time.April, 10, 1, 9, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-missing-state-repair",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindCommunityPost,
		ContentID:   "post-missing-state-repair",
		AlarmSentAt: firstAlarmSentAt,
	}}))

	_, err := db.Exec(ctx, `
		DELETE FROM youtube_community_shorts_alarm_states
		WHERE kind = $1 AND post_id = $2
	`, domain.OutboxKindCommunityPost, "community:post-missing-state-repair")
	require.NoError(t, err)

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindCommunityPost,
		ContentID:   "post-missing-state-repair",
		AlarmSentAt: laterAlarmSentAt,
	}}))

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "post-missing-state-repair")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "community:post-missing-state-repair", record.PostID)
	require.Equal(t, "post-missing-state-repair", record.ContentID)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, record.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, record.DeliveryStatus)
}

func TestRepositoryMarkAlarmSentBatchRejectsOldContentStateWhenCanonicalStateIsMissing(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 6, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, time.April, 10, 1, 8, 0, 0, time.UTC)
	laterAlarmSentAt := time.Date(2026, time.April, 10, 1, 9, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-legacy-state-repair",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AlarmSentAt:       &firstAlarmSentAt,
	}))

	_, err := db.Exec(ctx, `
		INSERT INTO youtube_community_shorts_alarm_states
			(kind, post_id, content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, delivery_status, created_at, updated_at)
		VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $9, $9)
	`, domain.OutboxKindCommunityPost, "post-legacy-state-repair", "post-legacy-state-repair", testChannelID, actualPublishedAt, detectedAt, firstAlarmSentAt, domain.YouTubeCommunityShortsAlarmStateStatusSent, detectedAt)
	require.NoError(t, err)

	err = repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindCommunityPost,
		ContentID:   "post-legacy-state-repair",
		AlarmSentAt: laterAlarmSentAt,
	}})
	require.ErrorContains(t, err, "idx_ycsas_kind_content")

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "post-legacy-state-repair")
	require.NoError(t, err)
	require.Nil(t, record)

	var oldStateAlarmSentAt time.Time

	require.NoError(t, db.QueryRow(ctx, `
		SELECT alarm_sent_at
		FROM youtube_community_shorts_alarm_states
		WHERE kind = $1 AND post_id = $2
	`, domain.OutboxKindCommunityPost, "post-legacy-state-repair").Scan(&oldStateAlarmSentAt))
	require.Equal(t, firstAlarmSentAt, oldStateAlarmSentAt.UTC())
}

func TestRepositoryUpsertAndFindAlarmStateByPostID(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            testCommunityPostID,
		ContentID:         testCommunityPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-1")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, domain.OutboxKindCommunityPost, record.Kind)
	require.Equal(t, "community:post-1", record.PostID)
	require.Equal(t, testCommunityPostID, record.ContentID)
	require.Equal(t, testChannelID, record.ChannelID)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, record.DetectedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, record.DeliveryStatus)
}

func TestRepositoryUpsertAlarmStatePreservesExistingActualPublishedAt(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	firstActualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	laterActualPublishedAt := firstActualPublishedAt.Add(5 * time.Minute)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-stable-published-at",
		ContentID:         "post-stable-published-at",
		ChannelID:         testChannelID,
		ActualPublishedAt: &firstActualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-stable-published-at",
		ContentID:         "post-stable-published-at",
		ChannelID:         testChannelID,
		ActualPublishedAt: &laterActualPublishedAt,
		DetectedAt:        detectedAt.Add(time.Minute),
	}))

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-stable-published-at")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, firstActualPublishedAt, record.ActualPublishedAt.UTC())
}

func TestRepositoryMarkAlarmSentBatchUpdatesAlarmState(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	alarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         testShortContentID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            testShortCanonicalPostID,
		ContentID:         testShortContentID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:        domain.OutboxKindNewShort,
		ContentID:   testShortCanonicalPostID,
		AlarmSentAt: alarmSentAt,
	}}))

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, testShortContentID)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, testShortCanonicalPostID, record.PostID)
	require.Equal(t, testShortContentID, record.ContentID)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, record.DeliveryStatus)
}

// 구 워커 claim이 남긴 미발송 ENQUEUED 행은 발송 표시로 SENT가 되고, 이미 발송된 행은 가장 이른 발송 시각을 유지한다.
func TestMarkAlarmSentBatchConvergesLegacyEnqueuedState(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	earlierSentAt := time.Date(2026, time.April, 10, 1, 4, 45, 0, time.UTC)
	alarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)

	testCases := []struct {
		contentID   string
		status      string
		existingAt  *time.Time
		wantAlarmAt time.Time
	}{
		{contentID: "post-legacy-enqueued", status: "ENQUEUED", wantAlarmAt: alarmSentAt},
		{contentID: "post-legacy-sent", status: "SENT", existingAt: &earlierSentAt, wantAlarmAt: earlierSentAt},
	}

	marks := make([]AlarmSentMark, 0, len(testCases))

	for _, tc := range testCases {
		require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         tc.contentID,
			ChannelID:         testChannelID,
			ActualPublishedAt: &actualPublishedAt,
			DetectedAt:        detectedAt,
		}))

		_, err := db.Exec(ctx, `
			INSERT INTO youtube_community_shorts_alarm_states
				(kind, post_id, content_id, channel_id, actual_published_at, detected_at, alarm_sent_at, delivery_status, created_at, updated_at)
			VALUES ($1, $2, $3, $4, $5, $6, $7, $8, $6, $6)`,
			domain.OutboxKindCommunityPost, "community:"+tc.contentID, tc.contentID, testChannelID, actualPublishedAt, detectedAt,
			tc.existingAt, tc.status)
		require.NoError(t, err)

		marks = append(marks, AlarmSentMark{Kind: domain.OutboxKindCommunityPost, ContentID: tc.contentID, AlarmSentAt: alarmSentAt})
	}

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, marks))

	for _, tc := range testCases {
		var (
			gotAlarmSentAt time.Time
			gotStatus      string
		)

		require.NoError(t, db.QueryRow(ctx, `
			SELECT alarm_sent_at, delivery_status
			FROM youtube_community_shorts_alarm_states
			WHERE kind = $1 AND post_id = $2`,
			domain.OutboxKindCommunityPost, "community:"+tc.contentID).Scan(&gotAlarmSentAt, &gotStatus))
		require.Equal(t, tc.wantAlarmAt, gotAlarmSentAt.UTC(), tc.contentID)
		require.Equal(t, string(domain.YouTubeCommunityShortsAlarmStateStatusSent), gotStatus, tc.contentID)
	}
}

func requireDuplicateDeliveryFinalized(
	t *testing.T,
	db trackingDB,
	repository *PgxRepository,
	firstAlarmSentAt time.Time,
) {
	t.Helper()

	ctx := t.Context()

	state, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, testDuplicateDeliveryPostID)
	require.NoError(t, err)
	require.NotNil(t, state)
	require.Equal(t, testDuplicateDeliveryCanonicalPostID, state.PostID)
	require.NotNil(t, state.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, state.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, state.DeliveryStatus)

	tracking, err := repository.FindByIdentity(ctx, domain.OutboxKindCommunityPost, testDuplicateDeliveryCanonicalPostID)
	require.NoError(t, err)
	require.NotNil(t, tracking)
	require.NotNil(t, tracking.AlarmSentAt)
	require.Equal(t, firstAlarmSentAt, tracking.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, tracking.DeliveryStatus)

	var postLevelStates int

	require.NoError(t, db.QueryRow(ctx, `
		SELECT count(*)
		FROM youtube_community_shorts_alarm_states
		WHERE kind = $1
		  AND (post_id = $2 OR content_id = $3)
	`, domain.OutboxKindCommunityPost, testDuplicateDeliveryCanonicalPostID, testDuplicateDeliveryPostID).Scan(&postLevelStates))
	require.Equal(t, 1, postLevelStates)
}

func TestRepositoryDuplicateAlarmMarksFinalizeOnePostLevelSentState(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	firstAlarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	laterAlarmSentAt := firstAlarmSentAt.Add(10 * time.Second)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         testDuplicateDeliveryPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   testDuplicateDeliveryPostID,
			AlarmSentAt: laterAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   testDuplicateDeliveryCanonicalPostID,
			AlarmSentAt: firstAlarmSentAt,
		},
	}))

	requireDuplicateDeliveryFinalized(t, db, repository, firstAlarmSentAt)
}
