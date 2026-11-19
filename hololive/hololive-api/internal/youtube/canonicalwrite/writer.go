package canonicalwrite

import (
	"context"
	"errors"
	"fmt"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/tracking/observation"
)

const batchMaxSize = 50

func PersistVideosTx(
	ctx context.Context,
	tx dbx.Tx,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if tx == nil {
		return errors.New("persist videos: tx is nil")
	}

	if err := validateShortNotificationPublishedAt(videos, notifications); err != nil {
		return fmt.Errorf("validate short notifications: %w", err)
	}

	if err := persistReconciledVideosTx(ctx, tx, videos, notifications, trackingRows, watermark); err != nil {
		return fmt.Errorf("persist reconciled videos tx: %w", err)
	}

	return nil
}

func persistReconciledVideosTx(
	ctx context.Context,
	tx dbx.Querier,
	videos []*domain.YouTubeVideo,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	notifications, trackingRows, err := dropAlreadyKnownShortArtifacts(ctx, tx, notifications, trackingRows)
	if err != nil {
		return fmt.Errorf("drop already-known short artifacts: %w", err)
	}

	if err := batchUpsertVideos(ctx, tx, videos); err != nil {
		return fmt.Errorf("batch upsert videos: %w", err)
	}

	canonicalizeShortContentIDs(notifications, trackingRows)

	sourcePosts := buildShortSourcePosts(videos, trackingRows)
	if err := observation.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert short source posts: %w", err)
	}

	if err := persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "video", "short"); err != nil {
		return fmt.Errorf("persist tracking and watermark: %w", err)
	}

	return nil
}

func PersistCommunityPostsTx(
	ctx context.Context,
	tx dbx.Tx,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if tx == nil {
		return errors.New("persist community posts: tx is nil")
	}

	if err := validateCommunityNotificationPublishedAt(posts, notifications); err != nil {
		return fmt.Errorf("validate community notifications: %w", err)
	}

	if err := persistCommunityPostsTx(ctx, tx, posts, notifications, trackingRows, watermark); err != nil {
		return fmt.Errorf("persist community posts tx: %w", err)
	}

	return nil
}

func persistCommunityPostsTx(
	ctx context.Context,
	tx dbx.Querier,
	posts []*domain.YouTubeCommunityPost,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
) error {
	if err := batchUpsertCommunityPosts(ctx, tx, posts); err != nil {
		return fmt.Errorf("batch upsert community posts: %w", err)
	}

	sourcePosts := buildCommunitySourcePosts(posts, trackingRows)
	if err := observation.NewRepository(tx).UpsertSourcePostsBatch(ctx, sourcePosts); err != nil {
		return fmt.Errorf("upsert community source posts: %w", err)
	}

	if err := persistTrackingAndWatermark(ctx, tx, notifications, trackingRows, watermark, "community", "community"); err != nil {
		return fmt.Errorf("persist tracking and watermark: %w", err)
	}

	return nil
}

func persistTrackingAndWatermark(
	ctx context.Context,
	tx dbx.Querier,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
	watermark *domain.YouTubeContentWatermark,
	trackingLabel string,
	alarmStateLabel string,
) error {
	if err := batchInsertNotifications(ctx, tx, notifications); err != nil {
		return fmt.Errorf("batch insert notifications: %w", err)
	}

	if err := reconcileTrackingRowsWithPersistedSendState(ctx, tx, trackingRows); err != nil {
		return fmt.Errorf("reconcile tracking rows with persisted send state: %w", err)
	}

	if err := observation.NewRepository(tx).UpsertBatch(ctx, trackingRows); err != nil {
		return fmt.Errorf("upsert %s tracking: %w", trackingLabel, err)
	}

	alarmStates := buildCommunityShortsAlarmStates(trackingRows)
	if err := observation.NewRepository(tx).UpsertAlarmStateBatch(ctx, alarmStates); err != nil {
		return fmt.Errorf("upsert %s alarm states: %w", alarmStateLabel, err)
	}

	if err := upsertWatermark(ctx, tx, watermark); err != nil {
		return fmt.Errorf("upsert watermark: %w", err)
	}

	return nil
}
