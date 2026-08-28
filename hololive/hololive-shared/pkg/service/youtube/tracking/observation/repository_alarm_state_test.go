package observation

import (
	"sync"
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
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            testCommunityPostID,
		ContentID:         testCommunityPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
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
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
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
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
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
		AuthorizedAt:      &authorizedAt,
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

func TestRepositoryMarkAlarmSentBatchFinalizesMatchingClaimedAlarmState(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	alarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         "post-claim-finalize",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "community:post-claim-finalize",
		ContentID:         "post-claim-finalize",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:         domain.OutboxKindCommunityPost,
		ContentID:    "post-claim-finalize",
		AuthorizedAt: &authorizedAt,
		AlarmSentAt:  alarmSentAt,
	}}))

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "post-claim-finalize")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AuthorizedAt)
	require.NotNil(t, record.AlarmSentAt)
	require.Equal(t, alarmSentAt, record.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, record.DeliveryStatus)
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
	require.Nil(t, state.AuthorizedAt)
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
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)
	firstAlarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)
	laterAlarmSentAt := firstAlarmSentAt.Add(10 * time.Second)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindCommunityPost,
		ContentID:         testDuplicateDeliveryPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	claimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            testDuplicateDeliveryPostID,
		ContentID:         testDuplicateDeliveryPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	duplicateClaimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            testDuplicateDeliveryCanonicalPostID,
		ContentID:         testDuplicateDeliveryPostID,
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &otherAuthorizedAt,
	})
	require.NoError(t, err)
	require.False(t, duplicateClaimed)

	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:         domain.OutboxKindCommunityPost,
			ContentID:    testDuplicateDeliveryPostID,
			AuthorizedAt: &authorizedAt,
			AlarmSentAt:  laterAlarmSentAt,
		},
		{
			Kind:         domain.OutboxKindCommunityPost,
			ContentID:    testDuplicateDeliveryCanonicalPostID,
			AuthorizedAt: &authorizedAt,
			AlarmSentAt:  firstAlarmSentAt,
		},
	}))

	requireDuplicateDeliveryFinalized(t, db, repository, firstAlarmSentAt)
}

func TestNormalizeAlarmSentMarksKeepsClaimAcrossCanonicalAliases(t *testing.T) {
	t.Parallel()

	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	firstAlarmSentAt := authorizedAt.Add(30 * time.Second)
	laterAlarmSentAt := firstAlarmSentAt.Add(10 * time.Second)

	marks, err := normalizeAlarmSentMarks([]AlarmSentMark{
		{
			Kind:         domain.OutboxKindCommunityPost,
			ContentID:    testDuplicateDeliveryPostID,
			AuthorizedAt: &authorizedAt,
			AlarmSentAt:  laterAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   testDuplicateDeliveryCanonicalPostID,
			AlarmSentAt: firstAlarmSentAt,
		},
	})
	require.NoError(t, err)
	require.Len(t, marks, 1)
	require.NotNil(t, marks[0].AuthorizedAt)
	require.Equal(t, authorizedAt, *marks[0].AuthorizedAt)
	require.Equal(t, firstAlarmSentAt, marks[0].AlarmSentAt)
}

func TestRepositoryMarkAlarmSentBatchRollsBackOnClaimAuthorizationMismatch(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)
	alarmSentAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
		Kind:              domain.OutboxKindNewShort,
		ContentID:         "short-claim-mismatch",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))
	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            "short:short-claim-mismatch",
		ContentID:         "short-claim-mismatch",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	err := repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{{
		Kind:         domain.OutboxKindNewShort,
		ContentID:    "short-claim-mismatch",
		AuthorizedAt: &otherAuthorizedAt,
		AlarmSentAt:  alarmSentAt,
	}})
	require.ErrorContains(t, err, "claim authorization mismatch")

	trackingRow, trackingErr := repository.FindByIdentity(ctx, domain.OutboxKindNewShort, "short-claim-mismatch")
	require.NoError(t, trackingErr)
	require.NotNil(t, trackingRow)
	require.Nil(t, trackingRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusPending, trackingRow.DeliveryStatus)

	stateRow, stateErr := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short-claim-mismatch")
	require.NoError(t, stateErr)
	require.NotNil(t, stateRow)
	require.NotNil(t, stateRow.AuthorizedAt)
	require.Equal(t, authorizedAt, stateRow.AuthorizedAt.UTC())
	require.Nil(t, stateRow.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, stateRow.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateCreatesMissingRow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)

	claimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-create",
		ContentID:         "post-claim-create",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	})
	require.NoError(t, err)
	require.True(t, claimed)

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-claim-create")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "community:post-claim-create", record.PostID)
	require.Equal(t, "post-claim-create", record.ContentID)
	require.Equal(t, testChannelID, record.ChannelID)
	require.NotNil(t, record.ActualPublishedAt)
	require.Equal(t, actualPublishedAt, record.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, record.DetectedAt.UTC())
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateRejectsMismatchedPostAndContentIdentity(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)

	claimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindCommunityPost,
		PostID:       "community:post-claim-good",
		ContentID:    "post-claim-other",
		ChannelID:    testChannelID,
		DetectedAt:   detectedAt,
		AuthorizedAt: &authorizedAt,
	})
	require.ErrorContains(t, err, "post id/content id mismatch")
	require.False(t, claimed)
}

func TestRepositoryTryClaimAlarmStateReturnsFalseForAlreadyClaimedRow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	firstAuthorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	laterAuthorizedAt := firstAuthorizedAt.Add(30 * time.Second)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-existing",
		ContentID:         "post-claim-existing",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &firstAuthorizedAt,
	}))

	claimed, err := repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-claim-existing",
		ContentID:         "post-claim-existing",
		ChannelID:         testChannelID,
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &laterAuthorizedAt,
	})
	require.NoError(t, err)
	require.False(t, claimed)

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-claim-existing")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, firstAuthorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}

func TestRepositoryTryClaimAlarmStateConcurrentCASClaimsDetectedRowOnce(t *testing.T) {
	repository := NewRepository(newTrackingTestDBWithMaxOpenConns(t, 8))
	ctx := t.Context()
	actualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 3, 0, time.UTC)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindNewShort,
		PostID:            "short-claim-race",
		ContentID:         "short-claim-race",
		ChannelID:         "UC_RACE",
		ActualPublishedAt: &actualPublishedAt,
		DetectedAt:        detectedAt,
	}))

	const contenders = 8

	attemptedAuthorizedAt := make([]time.Time, contenders)
	claimedResults := make([]bool, contenders)
	errResults := make([]error, contenders)
	start := make(chan struct{})

	var wg sync.WaitGroup

	for i := range contenders {
		attemptedAuthorizedAt[i] = detectedAt.Add(time.Duration(i+1) * time.Second)

		wg.Go(func() {
			<-start

			claimedResults[i], errResults[i] = repository.TryClaimAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
				Kind:              domain.OutboxKindNewShort,
				PostID:            "short-claim-race",
				ContentID:         "short-claim-race",
				ChannelID:         "UC_RACE",
				ActualPublishedAt: &actualPublishedAt,
				DetectedAt:        detectedAt,
				AuthorizedAt:      &attemptedAuthorizedAt[i],
			})
		})
	}

	close(start)
	wg.Wait()

	successCount := 0

	for i := range contenders {
		require.NoError(t, errResults[i])

		if claimedResults[i] {
			successCount++
		}
	}

	require.Equal(t, 1, successCount)

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:short-claim-race")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)

	matchedAttempt := false

	for i := range contenders {
		if record.AuthorizedAt.UTC().Equal(attemptedAuthorizedAt[i]) {
			matchedAttempt = true
			break
		}
	}

	require.True(t, matchedAttempt)
}

func TestRepositoryReleaseAlarmStateClaimClearsMatchingUnsentAuthorization(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindCommunityPost,
		PostID:         "post-release-claim",
		ContentID:      "post-release-claim",
		ChannelID:      testChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}))

	released, err := repository.ReleaseAlarmStateClaim(ctx, domain.OutboxKindCommunityPost, "community:post-release-claim", authorizedAt)
	require.NoError(t, err)
	require.True(t, released)

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-release-claim")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Nil(t, record.AuthorizedAt)
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusDetected, record.DeliveryStatus)
}

func TestRepositoryReleaseAlarmStateClaimReturnsFalseForMismatchedAuthorization(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := t.Context()
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	authorizedAt := time.Date(2026, time.April, 10, 1, 4, 30, 0, time.UTC)
	otherAuthorizedAt := authorizedAt.Add(30 * time.Second)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short-release-mismatch",
		ContentID:      "short-release-mismatch",
		ChannelID:      testChannelID,
		DetectedAt:     detectedAt,
		AuthorizedAt:   &authorizedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
	}))

	released, err := repository.ReleaseAlarmStateClaim(ctx, domain.OutboxKindNewShort, "short:short-release-mismatch", otherAuthorizedAt)
	require.NoError(t, err)
	require.False(t, released)

	record, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:short-release-mismatch")
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.AuthorizedAt)
	require.Equal(t, authorizedAt, record.AuthorizedAt.UTC())
	require.Nil(t, record.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, record.DeliveryStatus)
}
