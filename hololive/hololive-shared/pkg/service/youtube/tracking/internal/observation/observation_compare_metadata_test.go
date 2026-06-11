package observation

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestRepositoryEnrichObservationPostComparisonInputsLoadsTitleHintsAndPublishedAtFallbacks(t *testing.T) {
	repository := NewRepository(newTrackingTestDB(t))
	ctx := context.Background()

	communityPublishedAt := time.Date(2026, 4, 10, 1, 5, 0, 0, time.UTC)
	shortPublishedAt := time.Date(2026, 4, 10, 2, 10, 0, 0, time.UTC)
	_, err := dbx.ExecSQL(ctx, repository.db, "insert community post metadata", `
		INSERT INTO youtube_community_posts (post_id, channel_id, content_text, published_at)
		VALUES (?, ?, ?, ?)
	`, "UgkxMeta123", "UC_COMMUNITY", " hello   world\nsecond line ", communityPublishedAt)
	require.NoError(t, err)
	_, err = dbx.ExecSQL(ctx, repository.db, "insert short metadata", `
		INSERT INTO youtube_videos (video_id, channel_id, title, published_at)
		VALUES (?, ?, ?, ?)
	`, "AbC123xyZ89", "UC_SHORT", "  Test   Short   Title  ", shortPublishedAt)
	require.NoError(t, err)

	inputs, err := repository.EnrichObservationPostComparisonInputs(ctx, []ObservationPostComparisonInput{
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
