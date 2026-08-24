package batchrepo

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type watermarkRowVersion struct {
	CTID          string
	Xmin          string
	UpdatedAt     time.Time
	Initialized   bool
	LastContentID string
}

func TestPgxBatchRepositoryIdenticalWatermarkDoesNotRewriteRow(t *testing.T) {
	db := newBatchTestDB(t, &domain.YouTubeContentWatermark{})
	repository := NewBatchRepository(db)
	ctx := t.Context()
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     "channel-watermark-write",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-a",
	}

	require.NoError(t, persistVideos(ctx, repository, nil, nil, watermark))

	first := readWatermarkRowVersion(t, db, watermark.ChannelID, watermark.WatermarkType)

	require.NoError(t, persistVideos(ctx, repository, nil, nil, watermark))

	second := readWatermarkRowVersion(t, db, watermark.ChannelID, watermark.WatermarkType)
	require.Equal(t, first, second)

	changed := *watermark

	changed.LastContentID = "video-b"
	require.NoError(t, persistVideos(ctx, repository, nil, nil, &changed))

	third := readWatermarkRowVersion(t, db, changed.ChannelID, changed.WatermarkType)
	require.Equal(t, changed.LastContentID, third.LastContentID)
	require.NotEqual(t, first.CTID, third.CTID)
	require.NotEqual(t, first.Xmin, third.Xmin)
}

func readWatermarkRowVersion(
	t *testing.T,
	db *batchTestDB,
	channelID string,
	watermarkType domain.WatermarkType,
) watermarkRowVersion {
	t.Helper()

	var version watermarkRowVersion

	require.NoError(t, db.QueryRow(t.Context(), `
		SELECT ctid::text, xmin::text, updated_at, initialized, COALESCE(last_content_id, '')
		FROM youtube_content_watermarks
		WHERE channel_id = $1 AND watermark_type = $2`, channelID, watermarkType).Scan(
		&version.CTID,
		&version.Xmin,
		&version.UpdatedAt,
		&version.Initialized,
		&version.LastContentID,
	))

	return version
}
