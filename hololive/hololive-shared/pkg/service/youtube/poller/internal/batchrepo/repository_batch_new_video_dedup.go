package batchrepo

import (
	"context"
	"fmt"
	"strings"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type knownVideoIDRow struct {
	VideoID string `db:"video_id"`
}

func (r *PgxBatchRepository) dropAlreadyKnownVideoNotifications(
	ctx context.Context,
	tx batchDB,
	notifications []*domain.YouTubeNotificationOutbox,
) ([]*domain.YouTubeNotificationOutbox, error) {
	videoIDs := collectNewVideoContentIDs(notifications)
	if len(videoIDs) == 0 {
		return notifications, nil
	}

	var rows []knownVideoIDRow
	if err := dbx.SelectSQL(ctx, tx, &rows, "query existing videos for new-video dedup",
		`SELECT video_id FROM youtube_videos WHERE video_id IN (`+dbx.InPlaceholders(len(videoIDs))+`)`,
		dbx.AnyArgs(videoIDs)...); err != nil {
		return nil, fmt.Errorf("query existing videos for new-video dedup: %w", err)
	}
	if len(rows) == 0 {
		return notifications, nil
	}

	known := make(map[string]struct{}, len(rows))
	for i := range rows {
		known[rows[i].VideoID] = struct{}{}
	}
	return filterOutKnownNewVideoNotifications(notifications, known), nil
}

func collectNewVideoContentIDs(notifications []*domain.YouTubeNotificationOutbox) []string {
	ids := make([]string, 0, len(notifications))
	seen := make(map[string]struct{}, len(notifications))
	for _, notification := range notifications {
		if notification == nil || notification.Kind != domain.OutboxKindNewVideo {
			continue
		}
		id := strings.TrimSpace(notification.ContentID)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	return ids
}

func filterOutKnownNewVideoNotifications(
	notifications []*domain.YouTubeNotificationOutbox,
	known map[string]struct{},
) []*domain.YouTubeNotificationOutbox {
	filtered := make([]*domain.YouTubeNotificationOutbox, 0, len(notifications))
	for _, notification := range notifications {
		if notification != nil && notification.Kind == domain.OutboxKindNewVideo {
			if _, ok := known[strings.TrimSpace(notification.ContentID)]; ok {
				continue
			}
		}
		filtered = append(filtered, notification)
	}
	return filtered
}
