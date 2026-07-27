package batchrepo

import (
	"context"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPgxBatchRepositoryDropsKnownShortArtifactsWithoutOutbox(t *testing.T) {
	db := newBatchTestDB(t,
		&domain.YouTubeVideo{},
		&domain.YouTubeNotificationOutbox{},
		&domain.YouTubeContentWatermark{},
	)
	repository := NewBatchRepository(db)
	ctx := context.Background()
	detectedAt := time.Date(2026, 7, 10, 13, 27, 30, 0, time.UTC)
	knownShort := &domain.YouTubeVideo{
		VideoID:   "known-short",
		ChannelID: "channel-1",
		Title:     "Known Short",
		IsShort:   true,
	}
	newShort := &domain.YouTubeVideo{
		VideoID:   "new-short",
		ChannelID: "channel-1",
		Title:     "New Short",
		IsShort:   true,
	}
	require.NoError(t, repository.PersistVideos(ctx, []*domain.YouTubeVideo{knownShort}, nil, nil, nil))

	require.NoError(t, repository.PersistVideos(ctx,
		[]*domain.YouTubeVideo{knownShort, newShort},
		[]*domain.YouTubeNotificationOutbox{
			{
				Kind:      domain.OutboxKindNewShort,
				ChannelID: "channel-1",
				ContentID: "short:known-short",
				Payload:   buildShortNotificationPayload(knownShort, "short:known-short"),
				Status:    domain.OutboxStatusPending,
			},
			{
				Kind:      domain.OutboxKindNewShort,
				ChannelID: "channel-1",
				ContentID: "short:new-short",
				Payload:   buildShortNotificationPayload(newShort, "short:new-short"),
				Status:    domain.OutboxStatusPending,
			},
		},
		[]*domain.YouTubeContentAlarmTracking{
			{Kind: domain.OutboxKindNewShort, ContentID: "short:known-short", ChannelID: "channel-1", DetectedAt: detectedAt},
			{Kind: domain.OutboxKindNewShort, ContentID: "short:new-short", ChannelID: "channel-1", DetectedAt: detectedAt},
		},
		nil,
	))

	var outboxRows []domain.YouTubeNotificationOutbox
	require.NoError(t, db.Order("id ASC").Find(&outboxRows).Error)
	require.Len(t, outboxRows, 1)
	require.Equal(t, "short:new-short", outboxRows[0].ContentID)

	var trackingRows []domain.YouTubeContentAlarmTracking
	require.NoError(t, db.Order("content_id ASC").Find(&trackingRows).Error)
	require.Len(t, trackingRows, 1)
	require.Equal(t, "short:new-short", trackingRows[0].ContentID)
}
