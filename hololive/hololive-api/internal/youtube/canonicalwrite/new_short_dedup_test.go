package canonicalwrite

import (
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	dbtest "github.com/kapu/hololive-dbtest"
	"github.com/kapu/hololive-shared/pkg/domain"
)

func TestPersistVideosTxDropsKnownShortArtifactsWithoutOutbox(t *testing.T) {
	pool := dbtest.NewPool(t)
	ctx := t.Context()
	detectedAt := time.Date(2026, time.July, 10, 13, 27, 30, 0, time.UTC)
	knownShort := &domain.YouTubeVideo{
		VideoID:   "known-short",
		ChannelID: testChannelID,
		Title:     "Known Short",
		IsShort:   true,
	}
	newShort := &domain.YouTubeVideo{
		VideoID:   "new-short",
		ChannelID: testChannelID,
		Title:     "New Short",
		IsShort:   true,
	}

	require.NoError(t, commitVideos(ctx, pool, []*domain.YouTubeVideo{knownShort}, nil, nil, nil))

	require.NoError(t, commitVideos(ctx, pool,
		[]*domain.YouTubeVideo{knownShort, newShort},
		[]*domain.YouTubeNotificationOutbox{
			{
				Kind:      domain.OutboxKindNewShort,
				ChannelID: testChannelID,
				ContentID: "short:known-short",
				Payload:   shortPayload(t, knownShort, "short:known-short"),
				Status:    domain.OutboxStatusPending,
			},
			{
				Kind:      domain.OutboxKindNewShort,
				ChannelID: testChannelID,
				ContentID: "short:new-short",
				Payload:   shortPayload(t, newShort, "short:new-short"),
				Status:    domain.OutboxStatusPending,
			},
		},
		[]*domain.YouTubeContentAlarmTracking{
			{Kind: domain.OutboxKindNewShort, ContentID: "short:known-short", ChannelID: testChannelID, DetectedAt: detectedAt},
			{Kind: domain.OutboxKindNewShort, ContentID: "short:new-short", ChannelID: testChannelID, DetectedAt: detectedAt},
		},
		nil,
	))

	outboxRows := selectRows[domain.YouTubeNotificationOutbox](t, pool, `SELECT `+outboxColumns+` FROM youtube_notification_outbox ORDER BY id ASC`)
	require.Len(t, outboxRows, 1)
	require.Equal(t, "short:new-short", outboxRows[0].ContentID)

	trackingRows := selectRows[domain.YouTubeContentAlarmTracking](t, pool, `SELECT `+trackingColumns+` FROM youtube_content_alarm_tracking ORDER BY content_id ASC`)
	require.Len(t, trackingRows, 1)
	require.Equal(t, "short:new-short", trackingRows[0].ContentID)
}
