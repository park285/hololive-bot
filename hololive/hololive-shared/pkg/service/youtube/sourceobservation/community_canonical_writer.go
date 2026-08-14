package sourceobservation

import (
	"context"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/community"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime/batchrepo"
)

type batchCanonicalWriter struct {
	repo *batchrepo.PgxBatchRepository
}

func NewBatchCanonicalWriter(repo *batchrepo.PgxBatchRepository) CanonicalWriter {
	if repo == nil {
		return nil
	}
	return batchCanonicalWriter{repo: repo}
}

func (w batchCanonicalWriter) PersistTx(ctx context.Context, tx dbx.Tx, batch community.Batch) error {
	return w.repo.PersistCommunityPostsTx(ctx, tx, batch.Posts, batch.Notifications, batch.Tracking, batch.Watermark)
}

func (w batchCanonicalWriter) AfterCommit(ctx context.Context, batch community.Batch) {
	w.repo.RecordCommunityLatencyAfterCommit(ctx, batch.Tracking)
}

func (w batchCanonicalWriter) PersistVideosTx(
	ctx context.Context,
	tx dbx.Tx,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	tracking []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	return w.repo.PersistVideosTx(ctx, tx, videos, notifications, tracking, watermark)
}

func (w batchCanonicalWriter) AfterCommitVideos(ctx context.Context, tracking []*domain.YouTubeContentAlarmTracking) {
	w.repo.RecordCommunityLatencyAfterCommit(ctx, tracking)
}
