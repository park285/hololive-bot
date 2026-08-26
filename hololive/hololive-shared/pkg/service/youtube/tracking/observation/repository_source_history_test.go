package observation

import (
	"sync"
	"testing"
	"time"

	"github.com/georgysavva/scany/v2/pgxscan"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func selectSourcePostsForTest(t *testing.T, db trackingDB) []domain.YouTubeCommunityShortsSourcePost {
	t.Helper()

	var rows []domain.YouTubeCommunityShortsSourcePost

	require.NoError(t, pgxscan.Select(t.Context(), db, &rows, `
		SELECT kind, post_id, channel_id, actual_published_at, detected_at, created_at, updated_at
		FROM youtube_community_shorts_source_posts
		ORDER BY kind ASC, post_id ASC
	`))

	return rows
}

func TestRepositoryUpsertSourcePostsConvergesOnCanonicalAndBackfillsPublishedAt(t *testing.T) {
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	shortDetectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)
	shortDetectedLaterAt := time.Date(2026, time.April, 10, 1, 7, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, time.April, 10, 1, 2, 30, 0, time.UTC)
	communityDetectedAt := time.Date(2026, time.April, 10, 1, 5, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:       domain.OutboxKindNewShort,
			PostID:     testShortContentID,
			ChannelID:  testShortChannelID,
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
			PostID:            testShortCanonicalPostID,
			ChannelID:         testShortChannelID,
			ActualPublishedAt: &shortPublishedAt,
			DetectedAt:        shortDetectedAt,
		},
	}))

	rows := selectSourcePostsForTest(t, db)
	require.Len(t, rows, 2)

	rowsByKey := make(map[string]domain.YouTubeCommunityShortsSourcePost, len(rows))
	for i := range rows {
		rowsByKey[string(rows[i].Kind)+":"+rows[i].PostID] = rows[i]
	}

	shortRow, ok := rowsByKey[string(domain.OutboxKindNewShort)+":short:short-1"]
	require.True(t, ok)
	require.Equal(t, testShortChannelID, shortRow.ChannelID)
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
	db := newTrackingTestDB(t)
	repository := NewRepository(db)
	ctx := t.Context()
	firstActualPublishedAt := time.Date(2026, time.April, 10, 1, 2, 30, 0, time.UTC)
	laterActualPublishedAt := firstActualPublishedAt.Add(5 * time.Minute)
	detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)

	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-stable-source",
			ChannelID:         testShortChannelID,
			ActualPublishedAt: &firstActualPublishedAt,
			DetectedAt:        detectedAt,
		},
	}))
	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindNewShort,
			PostID:            "short:short-stable-source",
			ChannelID:         testShortChannelID,
			ActualPublishedAt: &laterActualPublishedAt,
			DetectedAt:        detectedAt.Add(time.Minute),
		},
	}))

	rows := selectSourcePostsForTest(t, db)
	require.Len(t, rows, 1)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, firstActualPublishedAt, rows[0].ActualPublishedAt.UTC())
}

type trackingIdentityCase struct {
	name        string
	kind        domain.OutboxKind
	rawID       string
	canonicalID string
}

type convergedTrackingRow struct {
	actualPublishedAt   time.Time
	earliestDetectedAt  time.Time
	earliestAlarmSentAt time.Time
}

func requireConvergedTrackingRow(
	t *testing.T,
	db trackingDB,
	repository *PgxRepository,
	tc trackingIdentityCase,
	want convergedTrackingRow,
) {
	t.Helper()

	rows := selectTrackingRowsForTest(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, tc.kind, rows[0].Kind)
	require.Equal(t, tc.canonicalID, rows[0].CanonicalContentID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, want.actualPublishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, want.earliestDetectedAt, rows[0].DetectedAt.UTC())
	require.NotNil(t, rows[0].AlarmSentAt)
	require.Equal(t, want.earliestAlarmSentAt, rows[0].AlarmSentAt.UTC())
	require.Equal(t, domain.YouTubeContentAlarmDeliveryStatusSent, rows[0].DeliveryStatus)

	ctx := t.Context()

	recordByRawID, err := repository.FindByIdentity(ctx, tc.kind, tc.rawID)
	require.NoError(t, err)
	require.NotNil(t, recordByRawID)
	require.Equal(t, tc.canonicalID, recordByRawID.CanonicalContentID)

	recordByCanonicalID, err := repository.FindByIdentity(ctx, tc.kind, tc.canonicalID)
	require.NoError(t, err)
	require.NotNil(t, recordByCanonicalID)
	require.Equal(t, tc.canonicalID, recordByCanonicalID.CanonicalContentID)
}

func TestRepositoryUpsertKeepsSingleTrackingRowForRepeatedSaves(t *testing.T) {
	testCases := []trackingIdentityCase{
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
			ctx := t.Context()
			actualPublishedAt := time.Date(2026, time.April, 10, 1, 0, 0, 0, time.UTC)
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

			requireConvergedTrackingRow(t, db, repository, tc, convergedTrackingRow{
				actualPublishedAt:   actualPublishedAt,
				earliestDetectedAt:  earliestDetectedAt,
				earliestAlarmSentAt: earliestAlarmSentAt,
			})
		})
	}
}

func concurrentTrackingUpsertRecords(
	tc trackingIdentityCase,
	actualPublishedAt time.Time,
	earliestDetectedAt time.Time,
	earliestAlarmSentAt *time.Time,
	laterAlarmSentAt *time.Time,
) []*domain.YouTubeContentAlarmTracking {
	const channelID = "UC_CONCURRENT"

	return []*domain.YouTubeContentAlarmTracking{
		{
			Kind: tc.kind, ContentID: tc.rawID, ChannelID: channelID,
			DetectedAt: actualPublishedAt.Add(90 * time.Second),
		},
		{
			Kind: tc.kind, ContentID: tc.canonicalID, ChannelID: channelID,
			ActualPublishedAt: &actualPublishedAt,
			DetectedAt:        actualPublishedAt.Add(45 * time.Second),
		},
		{
			Kind: tc.kind, ContentID: tc.rawID, ChannelID: channelID,
			ActualPublishedAt: &actualPublishedAt,
			DetectedAt:        earliestDetectedAt,
		},
		{
			Kind: tc.kind, ContentID: tc.canonicalID, ChannelID: channelID,
			DetectedAt:  actualPublishedAt.Add(75 * time.Second),
			AlarmSentAt: laterAlarmSentAt,
		},
		{
			Kind: tc.kind, ContentID: tc.rawID, ChannelID: channelID,
			DetectedAt:  actualPublishedAt.Add(30 * time.Second),
			AlarmSentAt: earliestAlarmSentAt,
		},
		{
			Kind: tc.kind, ContentID: tc.canonicalID, ChannelID: channelID,
			DetectedAt: actualPublishedAt.Add(2 * time.Minute),
		},
	}
}

func TestRepositoryUpsertKeepsSingleTrackingRowForConcurrentSaves(t *testing.T) {
	testCases := []trackingIdentityCase{
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
			db := newTrackingTestDBWithMaxOpenConns(t, 1)

			repository := NewRepository(db)
			ctx := t.Context()
			actualPublishedAt := time.Date(2026, time.April, 10, 2, 0, 0, 0, time.UTC)
			earliestDetectedAt := actualPublishedAt.Add(15 * time.Second)
			earliestAlarmSentAt := actualPublishedAt.Add(80 * time.Second)
			laterAlarmSentAt := actualPublishedAt.Add(105 * time.Second)

			records := concurrentTrackingUpsertRecords(
				tc, actualPublishedAt, earliestDetectedAt, &earliestAlarmSentAt, &laterAlarmSentAt,
			)
			errCh := make(chan error, len(records))
			start := make(chan struct{})

			var wg sync.WaitGroup

			for _, record := range records {
				wg.Go(func() {
					<-start

					errCh <- repository.Upsert(ctx, record)
				})
			}

			close(start)
			wg.Wait()
			close(errCh)

			for err := range errCh {
				require.NoError(t, err)
			}

			requireConvergedTrackingRow(t, db, repository, tc, convergedTrackingRow{
				actualPublishedAt:   actualPublishedAt,
				earliestDetectedAt:  earliestDetectedAt,
				earliestAlarmSentAt: earliestAlarmSentAt,
			})
		})
	}
}

func concurrentUpsertTracking(t *testing.T, repository *PgxRepository, records []*domain.YouTubeContentAlarmTracking) {
	t.Helper()

	ctx := t.Context()
	errCh := make(chan error, len(records))
	start := make(chan struct{})

	var wg sync.WaitGroup

	for _, record := range records {
		wg.Go(func() {
			<-start

			errCh <- repository.Upsert(ctx, record)
		})
	}

	close(start)
	wg.Wait()
	close(errCh)

	for err := range errCh {
		require.NoError(t, err)
	}
}

func requireSingleCanonicalTrackingRow(t *testing.T, db trackingDB, kind domain.OutboxKind, canonicalID string) {
	t.Helper()

	rows := selectTrackingRowsForTest(t, db)
	require.Len(t, rows, 1)
	require.Equal(t, kind, rows[0].Kind)
	require.Equal(t, canonicalID, rows[0].CanonicalContentID)
}

func TestRepositoryUpsertConvergesOnCanonicalForMixedIdentityConcurrentSaves(t *testing.T) {
	testCases := []struct {
		name        string
		kind        domain.OutboxKind
		rawID       string
		canonicalID string
	}{
		{
			name:        "community post",
			kind:        domain.OutboxKindCommunityPost,
			rawID:       "post-mixed-1",
			canonicalID: "community:post-mixed-1",
		},
		{
			name:        "short",
			kind:        domain.OutboxKindNewShort,
			rawID:       "short-mixed-1",
			canonicalID: "short:short-mixed-1",
		},
	}

	for _, tc := range testCases {
		t.Run(tc.name, func(t *testing.T) {
			db := newTrackingTestDBWithMaxOpenConns(t, 8)
			repository := NewRepository(db)
			detectedAt := time.Date(2026, time.April, 10, 1, 4, 0, 0, time.UTC)

			contentIDForms := []string{tc.rawID, tc.canonicalID, tc.rawID, tc.canonicalID, tc.rawID, tc.canonicalID, tc.rawID, tc.canonicalID}
			records := make([]*domain.YouTubeContentAlarmTracking, 0, len(contentIDForms))

			for i, form := range contentIDForms {
				records = append(records, &domain.YouTubeContentAlarmTracking{
					Kind:       tc.kind,
					ContentID:  form,
					ChannelID:  "UC_MIXED",
					DetectedAt: detectedAt.Add(time.Duration(i) * time.Second),
				})
			}

			concurrentUpsertTracking(t, repository, records)
			requireSingleCanonicalTrackingRow(t, db, tc.kind, tc.canonicalID)
		})
	}
}
