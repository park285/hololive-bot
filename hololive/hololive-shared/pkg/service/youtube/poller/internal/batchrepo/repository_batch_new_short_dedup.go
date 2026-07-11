package batchrepo

import (
	"context"
	"fmt"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type knownShortVideoIDRow struct {
	VideoID string `db:"video_id"`
}

func (r *PgxBatchRepository) dropAlreadyKnownShortArtifacts(
	ctx context.Context,
	tx batchDB,
	notifications []*domain.YouTubeNotificationOutbox,
	trackingRows []*domain.YouTubeContentAlarmTracking,
) ([]*domain.YouTubeNotificationOutbox, []*domain.YouTubeContentAlarmTracking, error) {
	videoIDs := collectShortNotificationVideoIDs(notifications)
	if len(videoIDs) == 0 {
		return notifications, trackingRows, nil
	}

	var rows []knownShortVideoIDRow
	query := mustSQL("repository_batch_new_short_dedup_0048_01.sql") +
		dbx.InPlaceholders(len(videoIDs)) +
		mustSQL("repository_batch_new_short_dedup_0082_02.sql")
	if err := dbx.SelectSQL(ctx, tx, &rows, "query known shorts without outbox", query, dbx.AnyArgs(videoIDs)...); err != nil {
		return nil, nil, fmt.Errorf("query known shorts without outbox: %w", err)
	}
	if len(rows) == 0 {
		return notifications, trackingRows, nil
	}

	known := make(map[string]struct{}, len(rows))
	for i := range rows {
		known[rows[i].VideoID] = struct{}{}
	}
	return filterKnownShortNotifications(notifications, known), filterKnownShortTrackingRows(trackingRows, known), nil
}

func collectShortNotificationVideoIDs(notifications []*domain.YouTubeNotificationOutbox) []string {
	ids := make([]string, 0, len(notifications))
	seen := make(map[string]struct{}, len(notifications))
	for _, notification := range notifications {
		if notification == nil || notification.Kind != domain.OutboxKindNewShort {
			continue
		}
		videoID := normalizeShortVideoResourceID(notification.ContentID)
		if videoID == "" {
			continue
		}
		if _, ok := seen[videoID]; ok {
			continue
		}
		seen[videoID] = struct{}{}
		ids = append(ids, videoID)
	}
	return ids
}

func filterKnownShortNotifications(
	notifications []*domain.YouTubeNotificationOutbox,
	known map[string]struct{},
) []*domain.YouTubeNotificationOutbox {
	filtered := make([]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	for _, notification := range notifications {
		if notification != nil && notification.Kind == domain.OutboxKindNewShort {
			if _, ok := known[normalizeShortVideoResourceID(notification.ContentID)]; ok {
				continue
			}
		}
		filtered = append(filtered, notification)
	}
	return filtered
}

func filterKnownShortTrackingRows(
	trackingRows []*domain.YouTubeContentAlarmTracking,
	known map[string]struct{},
) []*domain.YouTubeContentAlarmTracking {
	filtered := make([]*domain.YouTubeContentAlarmTracking, 0, len(trackingRows))
	for _, trackingRow := range trackingRows {
		if trackingRow != nil && trackingRow.Kind == domain.OutboxKindNewShort {
			if _, ok := known[normalizeShortVideoResourceID(trackingRow.ContentID)]; ok {
				continue
			}
		}
		filtered = append(filtered, trackingRow)
	}
	return filtered
}
