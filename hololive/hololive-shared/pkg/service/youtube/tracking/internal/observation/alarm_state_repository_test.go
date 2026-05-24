package observation

import (
	"context"
	"database/sql"
	"testing"
	"time"

	"github.com/stretchr/testify/require"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestFindAlarmStateByPostIDReturnsNilOnMissing(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))

	row, err := repository.FindAlarmStateByPostID(context.Background(), domain.OutboxKindCommunityPost, "community:missing")

	require.NoError(t, err)
	require.Nil(t, row)
}

func TestFindAlarmStateByPostIDReturnsExisting(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 2, 0, 0, time.UTC)
	detectedAt := publishedAt.Add(30 * time.Second)
	authorizedAt := publishedAt.Add(45 * time.Second)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:              domain.OutboxKindCommunityPost,
		PostID:            "post-existing",
		ContentID:         "post-existing",
		ChannelID:         "UC_EXISTING",
		ActualPublishedAt: &publishedAt,
		DetectedAt:        detectedAt,
		AuthorizedAt:      &authorizedAt,
	}))

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "community:post-existing")

	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, domain.OutboxKindCommunityPost, row.Kind)
	require.Equal(t, "community:post-existing", row.PostID)
	require.Equal(t, "post-existing", row.ContentID)
	require.Equal(t, "UC_EXISTING", row.ChannelID)
	require.NotNil(t, row.ActualPublishedAt)
	require.Equal(t, publishedAt, row.ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, row.DetectedAt.UTC())
	require.NotNil(t, row.AuthorizedAt)
	require.Equal(t, authorizedAt, row.AuthorizedAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusEnqueued, row.DeliveryStatus)
}

func TestMarkPublishedAtRetryAfterUpdatesColumn(t *testing.T) {
	db := newAlarmStateRepositoryTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	retryAfter := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:retry-after",
		ContentID:      "retry-after",
		ChannelID:      "UC_SHORT",
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}))

	require.NoError(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "retry-after", retryAfter))

	var row domain.YouTubeCommunityShortsAlarmState
	require.NoError(t, db.
		Select("published_at_retry_after").
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:retry-after").
		Take(&row).Error)
	require.NotNil(t, row.PublishedAtRetryAfter)
	require.Equal(t, retryAfter, row.PublishedAtRetryAfter.UTC())
}

func TestClearPublishedAtRetryAfterNullifiesColumn(t *testing.T) {
	db := newAlarmStateRepositoryTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	retryAfter := detectedAt.Add(2 * time.Minute)

	require.NoError(t, repository.UpsertAlarmState(ctx, &domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindNewShort,
		PostID:         "short:retry-clear",
		ContentID:      "retry-clear",
		ChannelID:      "UC_SHORT",
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}))
	require.NoError(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:retry-clear", retryAfter))

	require.NoError(t, repository.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "retry-clear"))

	var retryAfterColumn sql.NullTime
	require.NoError(t, db.
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Select("published_at_retry_after").
		Where("kind = ? AND post_id = ?", domain.OutboxKindNewShort, "short:retry-clear").
		Scan(&retryAfterColumn).Error)
	require.False(t, retryAfterColumn.Valid)
}

func TestListPendingPublishedAtResolutionsPageReturnsReadyCandidates(t *testing.T) {
	db := newAlarmStateRepositoryTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(5 * time.Minute)
	detectedBefore := referenceNow.Add(time.Minute)
	expiredRetryAfter := referenceNow.Add(-time.Minute)
	futureRetryAfter := referenceNow.Add(time.Minute)
	actualPublishedAt := detectedAt.Add(-time.Minute)
	authorizedAt := detectedAt.Add(30 * time.Second)

	rows := []domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindCommunityPost,
			PostID:         "community:ready-detected",
			ContentID:      "ready-detected",
			ChannelID:      "UC_READY",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:                  domain.OutboxKindNewShort,
			PostID:                "short:ready-retry",
			ContentID:             "ready-retry",
			ChannelID:             "UC_READY",
			DetectedAt:            detectedAt.Add(time.Second),
			PublishedAtRetryAfter: &expiredRetryAfter,
			DeliveryStatus:        domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindCommunityPost,
			PostID:         "community:ready-authorized",
			ContentID:      "ready-authorized",
			ChannelID:      "UC_READY",
			DetectedAt:     detectedAt.Add(2 * time.Second),
			AuthorizedAt:   &authorizedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:has-published-at",
			ContentID:         "has-published-at",
			ChannelID:         "UC_EXCLUDED",
			ActualPublishedAt: &actualPublishedAt,
			DetectedAt:        detectedAt,
			DeliveryStatus:    domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindCommunityPost,
			PostID:         "community:too-new",
			ContentID:      "too-new",
			ChannelID:      "UC_EXCLUDED",
			DetectedAt:     detectedBefore,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:                  domain.OutboxKindNewShort,
			PostID:                "short:future-retry",
			ContentID:             "future-retry",
			ChannelID:             "UC_EXCLUDED",
			DetectedAt:            detectedAt,
			PublishedAtRetryAfter: &futureRetryAfter,
			DeliveryStatus:        domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindNewVideo,
			PostID:         "video-ignored",
			ContentID:      "video-ignored",
			ChannelID:      "UC_EXCLUDED",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	candidates, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, detectedBefore, nil, 10)

	require.NoError(t, err)
	require.Nil(t, cursor)
	require.Len(t, candidates, 3)
	require.Equal(t, []PublishedAtResolutionCandidate{
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:ready-detected",
			ContentID:  "community:ready-detected",
			ChannelID:  "UC_READY",
			DetectedAt: detectedAt,
		},
		{
			Kind:       domain.OutboxKindNewShort,
			PostID:     "short:ready-retry",
			ContentID:  "short:ready-retry",
			ChannelID:  "UC_READY",
			DetectedAt: detectedAt.Add(time.Second),
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:ready-authorized",
			ContentID:  "community:ready-authorized",
			ChannelID:  "UC_READY",
			DetectedAt: detectedAt.Add(2 * time.Second),
		},
	}, candidates)
}

func TestListPendingPublishedAtResolutionsPageCursorPagination(t *testing.T) {
	db := newAlarmStateRepositoryTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	referenceNow := detectedAt.Add(5 * time.Minute)
	authorizedAt := detectedAt.Add(time.Minute)

	rows := []domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:page-a",
			ContentID:      "page-a",
			ChannelID:      "UC_PAGE",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:page-b",
			ContentID:      "page-b",
			ChannelID:      "UC_PAGE",
			DetectedAt:     detectedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindNewShort,
			PostID:         "short:page-c",
			ContentID:      "page-c",
			ChannelID:      "UC_PAGE",
			DetectedAt:     detectedAt.Add(time.Second),
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
		},
		{
			Kind:           domain.OutboxKindCommunityPost,
			PostID:         "community:page-authorized",
			ContentID:      "page-authorized",
			ChannelID:      "UC_PAGE",
			DetectedAt:     detectedAt.Add(2 * time.Second),
			AuthorizedAt:   &authorizedAt,
			DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusEnqueued,
		},
	}
	require.NoError(t, db.Create(&rows).Error)

	firstPage, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, referenceNow.Add(time.Minute), nil, 2)
	require.NoError(t, err)
	require.Len(t, firstPage, 2)
	require.NotNil(t, cursor)
	require.Equal(t, []string{"short:page-a", "short:page-b"}, publishedAtResolutionPostIDs(firstPage))
	require.Equal(t, 0, cursor.PriorityBucket)
	require.Equal(t, detectedAt, cursor.DetectedAt.UTC())
	require.Equal(t, "short:page-b", cursor.PostID)

	secondPage, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, referenceNow.Add(time.Minute), cursor, 2)
	require.NoError(t, err)
	require.Len(t, secondPage, 2)
	require.NotNil(t, cursor)
	require.Equal(t, []string{"short:page-c", "community:page-authorized"}, publishedAtResolutionPostIDs(secondPage))
	require.Equal(t, 1, cursor.PriorityBucket)
	require.Equal(t, detectedAt.Add(2*time.Second), cursor.DetectedAt.UTC())
	require.Equal(t, "community:page-authorized", cursor.PostID)

	thirdPage, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, referenceNow, referenceNow.Add(time.Minute), cursor, 2)
	require.NoError(t, err)
	require.Nil(t, thirdPage)
	require.Nil(t, cursor)
}

func TestListPendingPublishedAtResolutionsReturnsReadyCandidates(t *testing.T) {
	db := newAlarmStateRepositoryTestDB(t)
	repository := NewRepository(db)
	ctx := context.Background()
	detectedAt := time.Now().UTC().Add(-10 * time.Minute)

	require.NoError(t, db.Create(&domain.YouTubeCommunityShortsAlarmState{
		Kind:           domain.OutboxKindCommunityPost,
		PostID:         "community:list-wrapper",
		ContentID:      "list-wrapper",
		ChannelID:      "UC_WRAPPER",
		DetectedAt:     detectedAt,
		DeliveryStatus: domain.YouTubeCommunityShortsAlarmStateStatusDetected,
	}).Error)

	candidates, err := repository.ListPendingPublishedAtResolutions(ctx, detectedAt.Add(time.Minute), 1)

	require.NoError(t, err)
	require.Len(t, candidates, 1)
	require.Equal(t, "community:list-wrapper", candidates[0].PostID)
	require.Equal(t, "community:list-wrapper", candidates[0].ContentID)
}

func TestListPendingPublishedAtResolutionsPageRejectsInvalidRequest(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))
	ctx := context.Background()
	now := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	tests := []struct {
		name           string
		referenceNow   time.Time
		detectedBefore time.Time
		limit          int
		wantError      string
	}{
		{
			name:           "empty detected_before",
			referenceNow:   now,
			detectedBefore: time.Time{},
			limit:          1,
			wantError:      "detected before is empty",
		},
		{
			name:           "empty reference_now",
			referenceNow:   time.Time{},
			detectedBefore: now,
			limit:          1,
			wantError:      "reference now is empty",
		},
		{
			name:           "non-positive limit",
			referenceNow:   now,
			detectedBefore: now,
			limit:          0,
			wantError:      "limit must be positive",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			candidates, cursor, err := repository.ListPendingPublishedAtResolutionsPage(
				ctx,
				tt.referenceNow,
				tt.detectedBefore,
				nil,
				tt.limit,
			)

			require.ErrorContains(t, err, tt.wantError)
			require.Nil(t, candidates)
			require.Nil(t, cursor)
		})
	}
}

func TestBuildPublishedAtResolutionCandidatesRejectsInvalidRows(t *testing.T) {
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	_, err := buildPublishedAtResolutionCandidates([]pendingResolutionRow{{
		Kind:       domain.OutboxKindCommunityPost,
		PostID:     "",
		ContentID:  "invalid-post-id",
		ChannelID:  "UC_INVALID",
		DetectedAt: detectedAt,
	}})
	require.ErrorContains(t, err, "row 0")
	require.ErrorContains(t, err, "content id is empty")

	_, err = buildPublishedAtResolutionCandidates([]pendingResolutionRow{{
		Kind:       domain.OutboxKindCommunityPost,
		PostID:     "community:invalid-content-id",
		ContentID:  "",
		ChannelID:  "UC_INVALID",
		DetectedAt: detectedAt,
	}})
	require.ErrorContains(t, err, "row 0")
	require.ErrorContains(t, err, "content id is empty")
}

func TestUpsertAlarmStateBatchMergesDuplicateRecords(t *testing.T) {
	repository := NewRepository(newAlarmStateRepositoryTestDB(t))
	ctx := context.Background()
	publishedAt := time.Date(2026, 4, 10, 1, 1, 0, 0, time.UTC)
	laterDetectedAt := publishedAt.Add(2 * time.Minute)
	earlierDetectedAt := publishedAt.Add(time.Minute)
	laterAuthorizedAt := publishedAt.Add(3 * time.Minute)
	earlierAuthorizedAt := publishedAt.Add(2 * time.Minute)
	alarmSentAt := publishedAt.Add(4 * time.Minute)

	require.NoError(t, repository.UpsertAlarmStateBatch(ctx, []*domain.YouTubeCommunityShortsAlarmState{
		{
			Kind:         domain.OutboxKindCommunityPost,
			PostID:       "merge-duplicate",
			ContentID:    "merge-duplicate",
			ChannelID:    "UC_FIRST",
			DetectedAt:   laterDetectedAt,
			AuthorizedAt: &laterAuthorizedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:merge-duplicate",
			ContentID:         "merge-duplicate-updated",
			ChannelID:         "UC_SECOND",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        earlierDetectedAt,
			AuthorizedAt:      &earlierAuthorizedAt,
			AlarmSentAt:       &alarmSentAt,
		},
	}))

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindCommunityPost, "merge-duplicate")
	require.NoError(t, err)
	require.NotNil(t, row)
	require.Equal(t, "community:merge-duplicate", row.PostID)
	require.Equal(t, "merge-duplicate-updated", row.ContentID)
	require.Equal(t, "UC_SECOND", row.ChannelID)
	require.NotNil(t, row.ActualPublishedAt)
	require.Equal(t, publishedAt, row.ActualPublishedAt.UTC())
	require.Equal(t, earlierDetectedAt, row.DetectedAt.UTC())
	require.NotNil(t, row.AuthorizedAt)
	require.Equal(t, earlierAuthorizedAt, row.AuthorizedAt.UTC())
	require.NotNil(t, row.AlarmSentAt)
	require.Equal(t, alarmSentAt, row.AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, row.DeliveryStatus)
}

func TestMergeNormalizedAlarmStateHandlesNilAndTimestampPriority(t *testing.T) {
	existingDetectedAt := time.Date(2026, 4, 10, 1, 2, 0, 0, time.UTC)
	nextDetectedAt := existingDetectedAt.Add(-time.Minute)
	existingAuthorizedAt := existingDetectedAt.Add(2 * time.Minute)
	nextAuthorizedAt := existingDetectedAt.Add(time.Minute)
	existingAlarmSentAt := existingDetectedAt.Add(4 * time.Minute)

	existing := &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindNewShort,
		PostID:       "short:merge-helper",
		ContentID:    "merge-helper",
		ChannelID:    "UC_EXISTING",
		DetectedAt:   existingDetectedAt,
		AuthorizedAt: &existingAuthorizedAt,
		AlarmSentAt:  &existingAlarmSentAt,
	}
	next := &domain.YouTubeCommunityShortsAlarmState{
		Kind:         domain.OutboxKindNewShort,
		PostID:       "short:merge-helper",
		ContentID:    "merge-helper-updated",
		ChannelID:    "UC_NEXT",
		DetectedAt:   nextDetectedAt,
		AuthorizedAt: &nextAuthorizedAt,
	}

	require.Same(t, next, mergeNormalizedAlarmState(nil, next))
	require.Same(t, existing, mergeNormalizedAlarmState(existing, nil))

	merged := mergeNormalizedAlarmState(existing, next)

	require.Equal(t, "merge-helper-updated", merged.ContentID)
	require.Equal(t, "UC_NEXT", merged.ChannelID)
	require.Equal(t, nextDetectedAt, merged.DetectedAt)
	require.NotNil(t, merged.AuthorizedAt)
	require.Equal(t, nextAuthorizedAt, *merged.AuthorizedAt)
	require.NotNil(t, merged.AlarmSentAt)
	require.Equal(t, existingAlarmSentAt, *merged.AlarmSentAt)
	require.Equal(t, domain.YouTubeCommunityShortsAlarmStateStatusSent, merged.DeliveryStatus)
}

func TestAlarmStateRepositoryNilDBReturnsError(t *testing.T) {
	repository := NewRepository(nil)
	ctx := context.Background()
	now := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	row, err := repository.FindAlarmStateByPostID(ctx, domain.OutboxKindNewShort, "short:nil")
	require.Nil(t, row)
	require.ErrorContains(t, err, "db is nil")

	require.ErrorContains(t, repository.MarkPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:nil", now), "db is nil")
	require.ErrorContains(t, repository.ClearPublishedAtRetryAfter(ctx, domain.OutboxKindNewShort, "short:nil"), "db is nil")

	candidates, cursor, err := repository.ListPendingPublishedAtResolutionsPage(ctx, now, now.Add(time.Minute), nil, 1)
	require.ErrorContains(t, err, "db is nil")
	require.Nil(t, candidates)
	require.Nil(t, cursor)
}

func newAlarmStateRepositoryTestDB(t *testing.T) *gorm.DB {
	t.Helper()

	db, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{})
	require.NoError(t, err)
	sqlDB, err := db.DB()
	require.NoError(t, err)
	sqlDB.SetMaxOpenConns(1)
	require.NoError(t, db.AutoMigrate(&domain.YouTubeCommunityShortsAlarmState{}))
	return db
}

func publishedAtResolutionPostIDs(candidates []PublishedAtResolutionCandidate) []string {
	postIDs := make([]string, 0, len(candidates))
	for i := range candidates {
		postIDs = append(postIDs, candidates[i].PostID)
	}
	return postIDs
}
