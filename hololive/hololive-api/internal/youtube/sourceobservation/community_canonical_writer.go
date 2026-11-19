package sourceobservation

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-api/internal/youtube/canonicalwrite"
	"github.com/kapu/hololive-api/internal/youtube/community"
	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type canonicalWriter interface {
	PersistTx(context.Context, dbx.Tx, *community.Batch) error
	PersistVideosTx(context.Context, dbx.Tx, []*domain.YouTubeVideo, []*domain.YouTubeNotificationOutbox, []*domain.YouTubeContentAlarmTracking, *domain.YouTubeContentWatermark) error
}

type canonicalTxWriter struct{}

func (canonicalTxWriter) PersistTx(ctx context.Context, tx dbx.Tx, batch *community.Batch) error {
	if err := canonicalwrite.PersistCommunityPostsTx(ctx, tx, batch.Posts, batch.Notifications, batch.Tracking, batch.Watermark); err != nil {
		return fmt.Errorf("persist community posts tx: %w", err)
	}

	return nil
}

func (canonicalTxWriter) PersistVideosTx(
	ctx context.Context,
	tx dbx.Tx,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	tracking []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := canonicalwrite.PersistVideosTx(ctx, tx, videos, notifications, tracking, watermark); err != nil {
		return fmt.Errorf("persist videos tx: %w", err)
	}

	return nil
}
