package observation

import (
	"context"
	"sync"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryUpsertAndListSourcePostsWithinDetectedWindow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	shortDetectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)
	shortDetectedLaterAt := time.Date(2026, 4, 10, 1, 7, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 1, 2, 30, 0, time.UTC)
	communityDetectedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:       domain.OutboxKindNewShort,
			PostID:     "short-1",
			ChannelID:  "UC_SHORT",
			DetectedAt: shortDetectedLaterAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:post-1",
			ChannelID:  "UC_COMMUNITY",
			DetectedAt: communityDetectedAt,
		},
	}))
	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-1",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
	}))

	rows, err := repository.ListSourcePostsDetectedWithinWindow(ctx, windowStart, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	rowsByKey := make(map[string]domain.YouTubeCommunityShortsSourcePost, len(rows))
	for i := range rows {
		rowsByKey[string(rows[i].Kind)+":"+rows[i].PostID] = rows[i]
	}

	shortRow, ok := rowsByKey[string(domain.OutboxKindNewShort)+":short:short-1"]
	require.True(t, ok)
	require.Equal(t, "UC_SHORT", shortRow.ChannelID)
	require.NotNil(t, shortRow.ActualPublishedAt)
	require.Equal(t, shortPublishedAt, shortRow.ActualPublishedAt.UTC())
	require.Equal(t, shortDetectedAt, shortRow.DetectedAt.UTC())

	communityRow, ok := rowsByKey[string(domain.OutboxKindCommunityPost)+":community:post-1"]
	require.True(t, ok)
	require.Equal(t, "UC_COMMUNITY", communityRow.ChannelID)
	require.Nil(t, communityRow.ActualPublishedAt)
	require.Equal(t, communityDetectedAt, communityRow.DetectedAt.UTC())
}

func TestRepositoryUpsertSourcePostsPreservesExistingActualPublishedAt(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	firstActualPublishedAt := time.Date(2026, 4, 10, 1, 2, 30, 0, time.UTC)
	laterActualPublishedAt := firstActualPublishedAt.Add(5 * time.Minute)
	detectedAt := time.Date(2026, 4, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-stable-source",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &firstActualPublishedAt,
			DetectedAt:        detectedAt,
		},
	}))
	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-stable-source",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &laterActualPublishedAt,
			DetectedAt:        detectedAt.Add(time.Minute),
		},
	}))

	rows, err := repository.ListSourcePostsDetectedWithinWindow(ctx, detectedAt.Add(-time.Minute), detectedAt.Add(10*time.Minute))
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstActualPublishedAt, rows[0].ActualPublishedAt.UTC())
}

func TestRepositoryListSourcePostsWithinObservationWindowUsesPublishedAtAndDetectionCutoff(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()
	windowStart := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
	windowEnd := time.Date(2026, 4, 10, 1, 10, 0, 0, time.UTC)
	beforeWindowPublishedAt := windowStart.Add(-30 * time.Second)
	beforeWindowDetectedAt := windowStart.Add(time.Minute)
	fallbackDetectedAt := windowStart.Add(2 * time.Minute)
	includedPublishedAt := windowStart.Add(3 * time.Minute)
	includedDetectedAt := includedPublishedAt.Add(20 * time.Second)
	lateDetectedPublishedAt := windowStart.Add(4 * time.Minute)
	lateDetectedAt := windowEnd.Add(time.Minute)

	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:published-before-window",
			ChannelID:         "UC_BEFORE",
			ActualPublishedAt: &beforeWindowPublishedAt,
			DetectedAt:        beforeWindowDetectedAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			PostID:     "community:detected-fallback",
			ChannelID:  "UC_FALLBACK",
			DetectedAt: fallbackDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:included-published",
			ChannelID:         "UC_INCLUDED",
			ActualPublishedAt: &includedPublishedAt,
			DetectedAt:        includedDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:late-detected",
			ChannelID:         "UC_LATE",
			ActualPublishedAt: &lateDetectedPublishedAt,
			DetectedAt:        lateDetectedAt,
		},
	}))

	rows, err := repository.ListSourcePostsWithinObservationWindow(ctx, windowStart, windowEnd, windowEnd)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	rowsByPostID := make(map[string]domain.YouTubeCommunityShortsSourcePost, len(rows))
	for i := range rows {
		rowsByPostID[rows[i].PostID] = rows[i]
	}

	fallbackRow, ok := rowsByPostID["community:detected-fallback"]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindCommunityPost, fallbackRow.Kind)
	require.Equal(t, "UC_FALLBACK", fallbackRow.ChannelID)
	require.Nil(t, fallbackRow.ActualPublishedAt)
	require.Equal(t, fallbackDetectedAt, fallbackRow.DetectedAt.UTC())

	includedRow, ok := rowsByPostID["short:included-published"]
	require.True(t, ok)
	require.Equal(t, domain.OutboxKindNewShort, includedRow.Kind)
	require.Equal(t, "UC_INCLUDED", includedRow.ChannelID)
	require.NotNil(t, includedRow.ActualPublishedAt)
	require.Equal(t, includedPublishedAt, includedRow.ActualPublishedAt.UTC())
	require.Equal(t, includedDetectedAt, includedRow.DetectedAt.UTC())
}

func TestRepositoryListCommunityAlarmSentHistoriesByFinalizedObservationWindow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	firstCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-1")
	secondCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-2")
	pendingCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-pending")
	shortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-1")

	firstPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	firstDetectedAt := firstPublishedAt.Add(20 * time.Second)
	firstAlarmSentAt := firstPublishedAt.Add(65 * time.Second)
	secondPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	secondDetectedAt := secondPublishedAt.Add(15 * time.Second)
	secondAlarmSentAt := secondPublishedAt.Add(80 * time.Second)
	pendingDetectedAt := time.Date(2026, 4, 10, 3, 0, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)
	shortDetectedAt := shortPublishedAt.Add(10 * time.Second)
	shortAlarmSentAt := shortPublishedAt.Add(40 * time.Second)
	latePublishedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	lateDetectedAt := latePublishedAt.Add(10 * time.Second)
	lateAlarmSentAt := latePublishedAt.Add(time.Minute)

	require.NoError(t, repository.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-1",
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &firstPublishedAt,
			DetectedAt:        firstDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-2",
			ChannelID:         "UC_COMMUNITY_2",
			ActualPublishedAt: &secondPublishedAt,
			DetectedAt:        secondDetectedAt,
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			ContentID:  "community-pending",
			ChannelID:  "UC_COMMUNITY_PENDING",
			DetectedAt: pendingDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-1",
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-late",
			ChannelID:         "UC_COMMUNITY_LATE",
			ActualPublishedAt: &latePublishedAt,
			DetectedAt:        lateDetectedAt,
		},
	}))
	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-1",
			AlarmSentAt: firstAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-2",
			AlarmSentAt: secondAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-1",
			AlarmSentAt: shortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-late",
			AlarmSentAt: lateAlarmSentAt,
		},
	}))

	require.NoError(t, repository.db.Create([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            firstCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &firstPublishedAt,
			DetectedAt:        firstDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            secondCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_2",
			ActualPublishedAt: &secondPublishedAt,
			DetectedAt:        secondDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:      "youtube-producer",
			BigBangCutoverAt: cutoverAt,
			Kind:             domain.OutboxKindCommunityPost,
			PostID:           pendingCanonicalPostID,
			ChannelID:        "UC_COMMUNITY_PENDING",
			DetectedAt:       pendingDetectedAt,
			FinalizedAt:      finalizedAt,
		},
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            shortCanonicalPostID,
			ChannelID:         "UC_SHORT",
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
	}).Error)

	rows, err := repository.ListCommunityAlarmSentHistoriesByFinalizedObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, firstCanonicalPostID, rows[0].PostID)
	require.Equal(t, "community-post-1", rows[0].ContentID)
	require.Equal(t, "UC_COMMUNITY_1", rows[0].ChannelID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstPublishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, firstDetectedAt, rows[0].DetectedAt.UTC())
	require.Equal(t, firstAlarmSentAt, rows[0].AlarmSentAt.UTC())

	require.Equal(t, secondCanonicalPostID, rows[1].PostID)
	require.Equal(t, "community-post-2", rows[1].ContentID)
	require.Equal(t, "UC_COMMUNITY_2", rows[1].ChannelID)
	require.NotNil(t, rows[1].ActualPublishedAt)
	require.Equal(t, secondPublishedAt, rows[1].ActualPublishedAt.UTC())
	require.Equal(t, secondDetectedAt, rows[1].DetectedAt.UTC())
	require.Equal(t, secondAlarmSentAt, rows[1].AlarmSentAt.UTC())
}

func TestRepositoryListShortsAlarmSentHistoriesByFinalizedObservationWindow(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	cutoverAt := time.Date(2026, 4, 10, 0, 0, 0, 0, time.UTC)
	finalizedAt := time.Date(2026, 4, 11, 0, 0, 0, 0, time.UTC)
	communityCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindCommunityPost, "community-post-1")
	firstShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-1")
	secondShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-post-2")
	pendingShortCanonicalPostID := canonicalTrackingIdentity(domain.OutboxKindNewShort, "short-pending")

	communityPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	communityDetectedAt := communityPublishedAt.Add(20 * time.Second)
	communityAlarmSentAt := communityPublishedAt.Add(65 * time.Second)
	firstShortPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	firstShortDetectedAt := firstShortPublishedAt.Add(10 * time.Second)
	firstShortAlarmSentAt := firstShortPublishedAt.Add(55 * time.Second)
	secondShortPublishedAt := time.Date(2026, 4, 10, 3, 10, 0, 0, time.UTC)
	secondShortDetectedAt := secondShortPublishedAt.Add(12 * time.Second)
	secondShortAlarmSentAt := secondShortPublishedAt.Add(58 * time.Second)
	pendingShortDetectedAt := time.Date(2026, 4, 10, 4, 0, 0, 0, time.UTC)
	lateShortPublishedAt := time.Date(2026, 4, 11, 1, 0, 0, 0, time.UTC)
	lateShortDetectedAt := lateShortPublishedAt.Add(10 * time.Second)
	lateShortAlarmSentAt := lateShortPublishedAt.Add(time.Minute)

	require.NoError(t, repository.UpsertBatch(ctx, []*domain.YouTubeContentAlarmTracking{
		{
			Kind:              domain.OutboxKindCommunityPost,
			ContentID:         "community-post-1",
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-1",
			ChannelID:         "UC_SHORT_1",
			ActualPublishedAt: &firstShortPublishedAt,
			DetectedAt:        firstShortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-post-2",
			ChannelID:         "UC_SHORT_2",
			ActualPublishedAt: &secondShortPublishedAt,
			DetectedAt:        secondShortDetectedAt,
		},
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "short-pending",
			ChannelID:  "UC_SHORT_PENDING",
			DetectedAt: pendingShortDetectedAt,
		},
		{
			Kind:              domain.OutboxKindNewShort,
			ContentID:         "short-late",
			ChannelID:         "UC_SHORT_LATE",
			ActualPublishedAt: &lateShortPublishedAt,
			DetectedAt:        lateShortDetectedAt,
		},
	}))
	require.NoError(t, repository.MarkAlarmSentBatch(ctx, []AlarmSentMark{
		{
			Kind:        domain.OutboxKindCommunityPost,
			ContentID:   "community-post-1",
			AlarmSentAt: communityAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-1",
			AlarmSentAt: firstShortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-post-2",
			AlarmSentAt: secondShortAlarmSentAt,
		},
		{
			Kind:        domain.OutboxKindNewShort,
			ContentID:   "short-late",
			AlarmSentAt: lateShortAlarmSentAt,
		},
	}))

	require.NoError(t, repository.db.Create([]domain.YouTubeCommunityShortsObservationPostBaseline{
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            communityCanonicalPostID,
			ChannelID:         "UC_COMMUNITY_1",
			ActualPublishedAt: &communityPublishedAt,
			DetectedAt:        communityDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            firstShortCanonicalPostID,
			ChannelID:         "UC_SHORT_1",
			ActualPublishedAt: &firstShortPublishedAt,
			DetectedAt:        firstShortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:       "youtube-producer",
			BigBangCutoverAt:  cutoverAt,
			Kind:              domain.OutboxKindNewShort,
			PostID:            secondShortCanonicalPostID,
			ChannelID:         "UC_SHORT_2",
			ActualPublishedAt: &secondShortPublishedAt,
			DetectedAt:        secondShortDetectedAt,
			FinalizedAt:       finalizedAt,
		},
		{
			RuntimeName:      "youtube-producer",
			BigBangCutoverAt: cutoverAt,
			Kind:             domain.OutboxKindNewShort,
			PostID:           pendingShortCanonicalPostID,
			ChannelID:        "UC_SHORT_PENDING",
			DetectedAt:       pendingShortDetectedAt,
			FinalizedAt:      finalizedAt,
		},
	}).Error)

	rows, err := repository.ListShortsAlarmSentHistoriesByFinalizedObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	require.Equal(t, firstShortCanonicalPostID, rows[0].PostID)
	require.Equal(t, "short-post-1", rows[0].ContentID)
	require.Equal(t, "UC_SHORT_1", rows[0].ChannelID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstShortPublishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, firstShortDetectedAt, rows[0].DetectedAt.UTC())
	require.Equal(t, firstShortAlarmSentAt, rows[0].AlarmSentAt.UTC())

	require.Equal(t, secondShortCanonicalPostID, rows[1].PostID)
	require.Equal(t, "short-post-2", rows[1].ContentID)
	require.Equal(t, "UC_SHORT_2", rows[1].ChannelID)
	require.NotNil(t, rows[1].ActualPublishedAt)
	require.Equal(t, secondShortPublishedAt, rows[1].ActualPublishedAt.UTC())
	require.Equal(t, secondShortDetectedAt, rows[1].DetectedAt.UTC())
	require.Equal(t, secondShortAlarmSentAt, rows[1].AlarmSentAt.UTC())
}

func TestRepositoryUpsertKeepsSingleTrackingRowForRepeatedSaves(t *testing.T) {
	testCases := []struct {
		name        string
		kind        domain.OutboxKind
		rawID       string
		canonicalID string
	}{
		{
			name:        "community post",
			kind:        domain.OutboxKindCommunityPost,
			rawID:       "post-repeat-1",
			canonicalID: "community:post-repeat-1",
		},
		{
			name:        "short",
			kind:        domain.OutboxKindNewShort,
			rawID:       "short-repeat-1",
			canonicalID: "short:short-repeat-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTrackingTestDB(t)
			repository := NewRepository(db)
			ctx := context.Background()
			actualPublishedAt := time.Date(2026, 4, 10, 1, 0, 0, 0, time.UTC)
			earliestDetectedAt := actualPublishedAt.Add(30 * time.Second)
			laterDetectedAt := actualPublishedAt.Add(2 * time.Minute)
			earliestAlarmSentAt := actualPublishedAt.Add(75 * time.Second)
			laterAlarmSentAt := actualPublishedAt.Add(95 * time.Second)

			require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:       tc.kind,
				ContentID:  tc.rawID,
				ChannelID:  "UC_REPEAT",
				DetectedAt: laterDetectedAt,
			}))
			require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:              tc.kind,
				ContentID:         tc.canonicalID,
				ChannelID:         "UC_REPEAT",
				ActualPublishedAt: &actualPublishedAt,
				DetectedAt:        earliestDetectedAt,
			}))
			require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:        tc.kind,
				ContentID:   tc.rawID,
				ChannelID:   "UC_REPEAT",
				DetectedAt:  laterDetectedAt.Add(time.Minute),
				AlarmSentAt: &laterAlarmSentAt,
			}))
			require.NoError(t, repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
				Kind:        tc.kind,
				ContentID:   tc.canonicalID,
				ChannelID:   "UC_REPEAT",
				DetectedAt:  laterDetectedAt.Add(2 * time.Minute),
				AlarmSentAt: &earliestAlarmSentAt,
			}))

			var rows []domain.YouTubeContentAlarmTracking
			require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
			require.Len(t, rows, 1)
			require.Equal(t, tc.kind, rows[0].Kind)
			require.Equal(t, tc.canonicalID, rows[0].CanonicalContentID)
			require.NotNil(t, rows[0].ActualPublishedAt)
			require.Equal(t, actualPublishedAt, rows[0].ActualPublishedAt.UTC())
			require.Equal(t, earliestDetectedAt, rows[0].DetectedAt.UTC())
			require.NotNil(t, rows[0].AlarmSentAt)
			require.Equal(t, earliestAlarmSentAt, rows[0].AlarmSentAt.UTC())
			require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, rows[0].DeliveryStatus)

			recordByRawID, err := repository.FindByIdentity(ctx, tc.kind, tc.rawID)
			require.NoError(t, err)
			require.NotNil(t, recordByRawID)
			require.Equal(t, tc.canonicalID, recordByRawID.CanonicalContentID)

			recordByCanonicalID, err := repository.FindByIdentity(ctx, tc.kind, tc.canonicalID)
			require.NoError(t, err)
			require.NotNil(t, recordByCanonicalID)
			require.Equal(t, tc.canonicalID, recordByCanonicalID.CanonicalContentID)
		})
	}
}

func TestRepositoryUpsertKeepsSingleTrackingRowForConcurrentSaves(t *testing.T) {
	testCases := []struct {
		name        string
		kind        domain.OutboxKind
		rawID       string
		canonicalID string
	}{
		{
			name:        "community post",
			kind:        domain.OutboxKindCommunityPost,
			rawID:       "post-concurrent-1",
			canonicalID: "community:post-concurrent-1",
		},
		{
			name:        "short",
			kind:        domain.OutboxKindNewShort,
			rawID:       "short-concurrent-1",
			canonicalID: "short:short-concurrent-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTrackingTestDB(t)
			sqlDB, err := db.DB()
			require.NoError(t, err)
			sqlDB.SetMaxOpenConns(1)
			sqlDB.SetMaxIdleConns(1)

			repository := NewRepository(db)
			ctx := context.Background()
			actualPublishedAt := time.Date(2026, 4, 10, 2, 0, 0, 0, time.UTC)
			earliestDetectedAt := actualPublishedAt.Add(15 * time.Second)
			earliestAlarmSentAt := actualPublishedAt.Add(80 * time.Second)
			laterAlarmSentAt := actualPublishedAt.Add(105 * time.Second)

			variants := []struct {
				contentID         string
				actualPublishedAt *time.Time
				detectedAt        time.Time
				alarmSentAt       *time.Time
			}{
				{
					contentID:  tc.rawID,
					detectedAt: actualPublishedAt.Add(90 * time.Second),
				},
				{
					contentID:         tc.canonicalID,
					actualPublishedAt: &actualPublishedAt,
					detectedAt:        actualPublishedAt.Add(45 * time.Second),
				},
				{
					contentID:         tc.rawID,
					actualPublishedAt: &actualPublishedAt,
					detectedAt:        earliestDetectedAt,
				},
				{
					contentID:   tc.canonicalID,
					detectedAt:  actualPublishedAt.Add(75 * time.Second),
					alarmSentAt: &laterAlarmSentAt,
				},
				{
					contentID:   tc.rawID,
					detectedAt:  actualPublishedAt.Add(30 * time.Second),
					alarmSentAt: &earliestAlarmSentAt,
				},
				{
					contentID:  tc.canonicalID,
					detectedAt: actualPublishedAt.Add(2 * time.Minute),
				},
			}

			errCh := make(chan error, len(variants))
			start := make(chan struct{})
			var wg sync.WaitGroup
			wg.Add(len(variants))
			for _, variant := range variants {
				go func() {
					defer wg.Done()
					<-start
					errCh <- repository.Upsert(ctx, &domain.YouTubeContentAlarmTracking{
						Kind:              tc.kind,
						ContentID:         variant.contentID,
						ChannelID:         "UC_CONCURRENT",
						ActualPublishedAt: variant.actualPublishedAt,
						DetectedAt:        variant.detectedAt,
						AlarmSentAt:       variant.alarmSentAt,
					})
				}()
			}

			close(start)
			wg.Wait()
			close(errCh)
			for err := range errCh {
				require.NoError(t, err)
			}

			var rows []domain.YouTubeContentAlarmTracking
			require.NoError(t, db.Order("content_id ASC").Find(&rows).Error)
			require.Len(t, rows, 1)
			require.Equal(t, tc.kind, rows[0].Kind)
			require.Equal(t, tc.canonicalID, rows[0].CanonicalContentID)
			require.NotNil(t, rows[0].ActualPublishedAt)
			require.Equal(t, actualPublishedAt, rows[0].ActualPublishedAt.UTC())
			require.Equal(t, earliestDetectedAt, rows[0].DetectedAt.UTC())
			require.NotNil(t, rows[0].AlarmSentAt)
			require.Equal(t, earliestAlarmSentAt, rows[0].AlarmSentAt.UTC())
			require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, rows[0].DeliveryStatus)

			recordByRawID, err := repository.FindByIdentity(ctx, tc.kind, tc.rawID)
			require.NoError(t, err)
			require.NotNil(t, recordByRawID)
			require.Equal(t, tc.canonicalID, recordByRawID.CanonicalContentID)

			recordByCanonicalID, err := repository.FindByIdentity(ctx, tc.kind, tc.canonicalID)
			require.NoError(t, err)
			require.NotNil(t, recordByCanonicalID)
			require.Equal(t, tc.canonicalID, recordByCanonicalID.CanonicalContentID)
		})
	}
}
