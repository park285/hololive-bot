package canonicalwrite

import (
	"slices"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestBuildCommunityShortsAlarmStatesReturnsSortedRows(t *testing.T) {
	t.Parallel()

	now := time.Date(2026, time.April, 15, 12, 0, 0, 0, time.UTC)
	rows := buildCommunityShortsAlarmStates([]*domain.YouTubeContentAlarmTracking{
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "video-b",
			ChannelID:  "channel-b",
			DetectedAt: now.Add(2 * time.Minute),
		},
		{
			Kind:       domain.OutboxKindCommunityPost,
			ContentID:  "post-c",
			ChannelID:  "channel-c",
			DetectedAt: now.Add(time.Minute),
		},
		{
			Kind:       domain.OutboxKindNewShort,
			ContentID:  "video-a",
			ChannelID:  "channel-a",
			DetectedAt: now,
		},
	})

	require.Len(t, rows, 3)

	actualKeys := make([]string, 0, len(rows))
	for _, row := range rows {
		actualKeys = append(actualKeys, string(row.Kind)+"\x00"+row.PostID)
	}

	require.True(t, slices.IsSorted(actualKeys))
	require.Equal(t, []string{
		string(domain.OutboxKindCommunityPost) + "\x00" + normalizeContentID(domain.OutboxKindCommunityPost, "post-c"),
		string(domain.OutboxKindNewShort) + "\x00" + normalizeContentID(domain.OutboxKindNewShort, "video-a"),
		string(domain.OutboxKindNewShort) + "\x00" + normalizeContentID(domain.OutboxKindNewShort, "video-b"),
	}, actualKeys)
}

func TestNotificationChunksByKindDeduplicatesSameKindContentID(t *testing.T) {
	t.Parallel()

	notifications := []*domain.YouTubeNotificationOutbox{
		{
			Kind:      domain.OutboxKindNewVideo,
			ContentID: testVideoID,
			Payload:   `{"kind":"first"}`,
		},
		{
			Kind:      domain.OutboxKindNewShort,
			ContentID: testVideoID,
			Payload:   `{"kind":"short"}`,
		},
		{
			Kind:      domain.OutboxKindNewVideo,
			ContentID: testVideoID,
			Payload:   `{"kind":"duplicate"}`,
		},
	}

	chunks := notificationChunksByKind(notifications)

	require.Len(t, chunks, 2)
	require.Len(t, chunks[0], 1)
	require.Equal(t, domain.OutboxKindNewVideo, chunks[0][0].Kind)
	require.JSONEq(t, `{"kind":"first"}`, chunks[0][0].Payload)
	require.Len(t, chunks[1], 1)
	require.Equal(t, domain.OutboxKindNewShort, chunks[1][0].Kind)
}

type watermarkRowVersion struct {
	CTID          string
	Xmin          string
	UpdatedAt     time.Time
	Initialized   bool
	LastContentID string
}

func TestPersistVideosTxIdenticalWatermarkDoesNotRewriteRow(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	watermark := &domain.YouTubeContentWatermark{
		ChannelID:     "channel-watermark-write",
		WatermarkType: domain.WatermarkTypeVideo,
		Initialized:   true,
		LastContentID: "video-a",
	}
	readVersion := func(channelID string, watermarkType domain.WatermarkType) watermarkRowVersion {
		t.Helper()

		var version watermarkRowVersion

		require.NoError(t, pool.QueryRow(ctx, `
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

	require.NoError(t, commitVideos(ctx, pool, nil, nil, nil, watermark))

	first := readVersion(watermark.ChannelID, watermark.WatermarkType)

	require.NoError(t, commitVideos(ctx, pool, nil, nil, nil, watermark))
	require.Equal(t, first, readVersion(watermark.ChannelID, watermark.WatermarkType))

	changed := *watermark

	changed.LastContentID = "video-b"
	require.NoError(t, commitVideos(ctx, pool, nil, nil, nil, &changed))

	third := readVersion(changed.ChannelID, changed.WatermarkType)
	require.Equal(t, changed.LastContentID, third.LastContentID)
	require.NotEqual(t, first.CTID, third.CTID)
	require.NotEqual(t, first.Xmin, third.Xmin)
}
