package observation

import (
	"context"
	"testing"
	"time"

	"github.com/jackc/pgx/v5/pgxpool"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestObservationWindowRepositoryEnsureAndFind(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    deploymentCompletedAt.Add(24 * time.Hour),
	}))

	record, err := repository.FindCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "youtube-producer", record.RuntimeName)
	require.Equal(t, cutoverAt, record.BigBangCutoverAt.UTC())
	require.Equal(t, "2.0.0", record.AppVersion)
	require.Equal(t, 44, record.TargetChannelCount)
	require.Equal(t, deploymentCompletedAt, record.DeploymentCompletedAt.UTC())
	require.Equal(t, deploymentCompletedAt, record.ObservationStartedAt.UTC())
	require.Equal(t, deploymentCompletedAt.Add(24*time.Hour), record.ObservationEndedAt.UTC())
	require.Nil(t, record.ClosedAt)
}

func TestObservationWindowRepositoryEnsurePreservesEarliestDeploymentWindowOnReplay(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	firstCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	secondCompletedAt := firstCompletedAt.Add(90 * time.Minute)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: firstCompletedAt,
		ObservationStartedAt:  firstCompletedAt,
		ObservationEndedAt:    firstCompletedAt.Add(24 * time.Hour),
	}))
	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.1",
		TargetChannelCount:    46,
		DeploymentCompletedAt: secondCompletedAt,
		ObservationStartedAt:  secondCompletedAt,
		ObservationEndedAt:    secondCompletedAt.Add(24 * time.Hour),
	}))

	record, err := repository.FindCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.NotNil(t, record)
	require.Equal(t, "2.0.0", record.AppVersion)
	require.Equal(t, 46, record.TargetChannelCount)
	require.Equal(t, firstCompletedAt, record.DeploymentCompletedAt.UTC())
	require.Equal(t, firstCompletedAt, record.ObservationStartedAt.UTC())
	require.Equal(t, firstCompletedAt.Add(24*time.Hour), record.ObservationEndedAt.UTC())
	require.Nil(t, record.ClosedAt)
}

func TestObservationWindowRepositoryFindClosedClosesWindowAtEnd(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
	}))

	record, err := repository.FindClosedCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt, observationEndedAt.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, record)
	require.NotNil(t, record.ClosedAt)
	require.Equal(t, observationEndedAt, record.ClosedAt.UTC())

	reloaded, err := repository.FindCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.NotNil(t, reloaded)
	require.NotNil(t, reloaded.ClosedAt)
	require.Equal(t, observationEndedAt, reloaded.ClosedAt.UTC())
}

func TestObservationWindowRepositoryFindClosedRejectsOpenWindow(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
	}))

	record, err := repository.FindClosedCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt, observationEndedAt.Add(-time.Minute))
	require.Nil(t, record)
	require.ErrorContains(t, err, "still open")

	reloaded, findErr := repository.FindCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, findErr)
	require.NotNil(t, reloaded)
	require.Nil(t, reloaded.ClosedAt)
}

func TestObservationWindowRepositoryRejectsInvalidWindow(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	err := repository.EnsureCommunityShortsObservationWindow(context.Background(), &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC),
		ObservationStartedAt:  time.Date(2026, 4, 10, 1, 16, 0, 0, time.UTC),
		ObservationEndedAt:    time.Date(2026, 4, 11, 1, 16, 0, 0, time.UTC),
	})
	require.ErrorContains(t, err, "deployment completed at must match observation started at")
}

func TestObservationWindowRepositoryRejectsInvalidClosedAt(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)
	invalidClosedAt := observationEndedAt.Add(time.Minute)

	err := repository.EnsureCommunityShortsObservationWindow(context.Background(), &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC),
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
		ClosedAt:              &invalidClosedAt,
	})
	require.ErrorContains(t, err, "closed at must match observation ended at")
}

func TestObservationWindowRepositoryFindClosedFinalizesObservationPostBaselines(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 10, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 10, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)
	publishedAt := deploymentCompletedAt.Add(4 * time.Minute)
	detectedAt := publishedAt.Add(30 * time.Second)
	lateDetectedAt := observationEndedAt.Add(time.Minute)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
	}))
	require.NoError(t, repository.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{
		{
			Kind:              domain.OutboxKindCommunityPost,
			PostID:            "community:post-timely",
			ChannelID:         "UC_COMMUNITY",
			ActualPublishedAt: &publishedAt,
			DetectedAt:        detectedAt,
		},
		{
			Kind:       domain.OutboxKindNewShort,
			PostID:     "short:late-detected",
			ChannelID:  "UC_SHORT",
			DetectedAt: lateDetectedAt,
		},
	}))

	window, err := repository.FindClosedCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt, observationEndedAt.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, window)
	require.NotNil(t, window.FinalizedPostBaselineAt)
	require.Equal(t, observationEndedAt, window.FinalizedPostBaselineAt.UTC())
	require.Equal(t, 1, window.FinalizedPostCount)

	rows, err := repository.ListCommunityShortsObservationPostBaselines(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 1)
	require.Equal(t, domain.OutboxKindCommunityPost, rows[0].Kind)
	require.Equal(t, "community:post-timely", rows[0].PostID)
	require.Equal(t, "UC_COMMUNITY", rows[0].ChannelID)
	require.NotNil(t, rows[0].ActualPublishedAt)
	require.Equal(t, publishedAt, rows[0].ActualPublishedAt.UTC())
	require.Equal(t, detectedAt, rows[0].DetectedAt.UTC())
	require.Equal(t, observationEndedAt, rows[0].FinalizedAt.UTC())
}

func TestObservationWindowRepositoryFindClosedFinalizesEmptyObservationPostBaseline(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 11, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 11, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
	}))

	window, err := repository.FindClosedCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt, observationEndedAt.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, window)
	require.NotNil(t, window.FinalizedPostBaselineAt)
	require.Equal(t, observationEndedAt, window.FinalizedPostBaselineAt.UTC())
	require.Equal(t, 0, window.FinalizedPostCount)

	rows, err := repository.ListCommunityShortsObservationPostBaselines(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Empty(t, rows)
}

func TestObservationWindowRepositoryFindClosedFinalizesObservationPostBaselinesByPublishedWindow(t *testing.T) {
	t.Parallel()

	repository := NewRepository(newObservationWindowTestDB(t))
	ctx := context.Background()
	cutoverAt := time.Date(2026, 4, 12, 1, 11, 12, 0, time.UTC)
	deploymentCompletedAt := time.Date(2026, 4, 12, 1, 15, 0, 0, time.UTC)
	observationEndedAt := deploymentCompletedAt.Add(24 * time.Hour)
	beforeWindowPublishedAt := deploymentCompletedAt.Add(-time.Minute)
	beforeWindowDetectedAt := deploymentCompletedAt.Add(2 * time.Minute)
	fallbackDetectedAt := deploymentCompletedAt.Add(3 * time.Minute)
	includedPublishedAt := deploymentCompletedAt.Add(4 * time.Minute)
	includedDetectedAt := includedPublishedAt.Add(25 * time.Second)
	lateDetectionPublishedAt := deploymentCompletedAt.Add(5 * time.Minute)
	lateDetectedAt := observationEndedAt.Add(time.Minute)

	require.NoError(t, repository.EnsureCommunityShortsObservationWindow(ctx, &domain.YouTubeCommunityShortsObservationWindow{
		RuntimeName:           "youtube-producer",
		BigBangCutoverAt:      cutoverAt,
		AppVersion:            "2.0.0",
		TargetChannelCount:    44,
		DeploymentCompletedAt: deploymentCompletedAt,
		ObservationStartedAt:  deploymentCompletedAt,
		ObservationEndedAt:    observationEndedAt,
	}))
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
			ActualPublishedAt: &lateDetectionPublishedAt,
			DetectedAt:        lateDetectedAt,
		},
	}))

	window, err := repository.FindClosedCommunityShortsObservationWindow(ctx, "youtube-producer", cutoverAt, observationEndedAt.Add(time.Minute))
	require.NoError(t, err)
	require.NotNil(t, window)
	require.NotNil(t, window.FinalizedPostBaselineAt)
	require.Equal(t, observationEndedAt, window.FinalizedPostBaselineAt.UTC())
	require.Equal(t, 2, window.FinalizedPostCount)

	rows, err := repository.ListCommunityShortsObservationPostBaselines(ctx, "youtube-producer", cutoverAt)
	require.NoError(t, err)
	require.Len(t, rows, 2)

	rowsByPostID := make(map[string]domain.YouTubeCommunityShortsObservationPostBaseline, len(rows))
	for i := range rows {
		rowsByPostID[rows[i].PostID] = rows[i]
	}

	fallbackRow, ok := rowsByPostID["community:detected-fallback"]
	require.True(t, ok)
	require.Equal(t, "UC_FALLBACK", fallbackRow.ChannelID)
	require.Nil(t, fallbackRow.ActualPublishedAt)
	require.Equal(t, fallbackDetectedAt, fallbackRow.DetectedAt.UTC())
	require.Equal(t, observationEndedAt, fallbackRow.FinalizedAt.UTC())

	includedRow, ok := rowsByPostID["short:included-published"]
	require.True(t, ok)
	require.Equal(t, "UC_INCLUDED", includedRow.ChannelID)
	require.NotNil(t, includedRow.ActualPublishedAt)
	require.Equal(t, includedPublishedAt, includedRow.ActualPublishedAt.UTC())
	require.Equal(t, includedDetectedAt, includedRow.DetectedAt.UTC())
	require.Equal(t, observationEndedAt, includedRow.FinalizedAt.UTC())
}

func newObservationWindowTestDB(t *testing.T) *pgxpool.Pool {
	t.Helper()
	return newTrackingTestDB(t)
}
