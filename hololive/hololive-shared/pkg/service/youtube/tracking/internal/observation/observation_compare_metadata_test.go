package observation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryEnrichObservationPostComparisonInputsLoadsTitleHintsAndPublishedAtFallbacks(t *testing.T) {
	repo := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	require.NoError(t, repo.db.AutoMigrate(&domain.YouTubeCommunityPost{}, &domain.YouTubeVideo{}))

	communityPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	require.NoError(t, repo.db.Create(&domain.YouTubeCommunityPost{
		PostID:      "UgkxMeta123",
		ChannelID:   "UC_COMMUNITY",
		ContentText: " hello   world\nsecond line ",
		PublishedAt: &communityPublishedAt,
	}).Error)
	require.NoError(t, repo.db.Create(&domain.YouTubeVideo{
		VideoID:     "AbC123xyZ89",
		ChannelID:   "UC_SHORT",
		Title:       "  Test   Short   Title  ",
		PublishedAt: &shortPublishedAt,
	}).Error)

	inputs, err := repo.EnrichObservationPostComparisonInputs(ctx, []ObservationPostComparisonInput{
		{
			Kind:            domain.OutboxKindCommunityPost,
			CanonicalPostID: "community:UgkxMeta123",
			ChannelID:       "UC_COMMUNITY",
		},
		{
			Kind:            domain.OutboxKindNewShort,
			CanonicalPostID: "short:AbC123xyZ89",
			ContentID:       "AbC123xyZ89",
			ChannelID:       "UC_SHORT",
		},
	})
	require.NoError(t, err)
	require.Len(t, inputs, 2)

	require.Equal(t, "hello world second line", inputs[0].TitleHint)
	require.NotNil(t, inputs[0].ActualPublishedAt)
	require.Equal(t, communityPublishedAt, inputs[0].ActualPublishedAt.UTC())

	require.Equal(t, "Test Short Title", inputs[1].TitleHint)
	require.NotNil(t, inputs[1].ActualPublishedAt)
	require.Equal(t, shortPublishedAt, inputs[1].ActualPublishedAt.UTC())
}
