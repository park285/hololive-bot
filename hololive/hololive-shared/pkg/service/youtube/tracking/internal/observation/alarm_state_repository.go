package observation

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type pendingResolutionRow struct {
	PriorityBucket        int               `db:"priority_bucket"`
	Kind                  domain.OutboxKind `db:"kind"`
	PostID                string            `db:"post_id"`
	ContentID             string            `db:"content_id"`
	ChannelID             string            `db:"channel_id"`
	DetectedAt            time.Time         `db:"detected_at"`
	PublishedAtRetryAfter *time.Time        `db:"published_at_retry_after"`
}

func (r *alarmStateRepository) ListPendingPublishedAtResolutions(
	ctx context.Context,
	detectedBefore time.Time,
	limit int,
) ([]PublishedAtResolutionCandidate, error) {
	candidates, _, err := r.ListPendingPublishedAtResolutionsPage(ctx, time.Now(), detectedBefore, nil, limit)
	if err != nil {
		return nil, err
	}
	return candidates, nil
}

func (r *alarmStateRepository) ListPendingPublishedAtResolutionsPage(
	ctx context.Context,
	referenceNow time.Time,
	detectedBefore time.Time,
	cursor *PublishedAtResolutionCursor,
	limit int,
) ([]PublishedAtResolutionCandidate, *PublishedAtResolutionCursor, error) {
	if err := validatePendingPublishedAtResolutionPageRequest(r, detectedBefore, referenceNow, limit); err != nil {
		return nil, nil, err
	}
	rows, err := r.loadPendingPublishedAtResolutionRows(ctx, referenceNow, detectedBefore, cursor, limit)
	if err != nil {
		return nil, nil, err
	}
	candidates, err := buildPublishedAtResolutionCandidates(rows)
	if err != nil {
		return nil, nil, err
	}
	if len(candidates) == 0 {
		return nil, nil, nil
	}

	return candidates, buildPendingPublishedAtResolutionCursor(rows, candidates, limit), nil
}

func (r *alarmStateRepository) MarkPublishedAtRetryAfter(
	ctx context.Context,
	kind domain.OutboxKind,
	postID string,
	retryAfter time.Time,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("mark published_at retry after: db is nil")
	}
	if err := r.requirePublishedAtRetryAfterColumn("mark published_at retry after"); err != nil {
		return err
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return fmt.Errorf("mark published_at retry after: %w", err)
	}

	if _, err := dbx.ExecSQL(ctx, r.db, "mark published_at retry after", `
		UPDATE youtube_community_shorts_alarm_states
		SET published_at_retry_after = ?, updated_at = ?
		WHERE kind = ? AND post_id = ?
	`, yttimestamp.Normalize(retryAfter), yttimestamp.Normalize(time.Now()), normalizedKind, normalizedPostID); err != nil {
		return err
	}

	return nil
}

func (r *alarmStateRepository) ClearPublishedAtRetryAfter(
	ctx context.Context,
	kind domain.OutboxKind,
	postID string,
) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("clear published_at retry after: db is nil")
	}
	if err := r.requirePublishedAtRetryAfterColumn("clear published_at retry after"); err != nil {
		return err
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return fmt.Errorf("clear published_at retry after: %w", err)
	}

	if _, err := dbx.ExecSQL(ctx, r.db, "clear published_at retry after", `
		UPDATE youtube_community_shorts_alarm_states
		SET published_at_retry_after = NULL, updated_at = ?
		WHERE kind = ? AND post_id = ?
	`, yttimestamp.Normalize(time.Now()), normalizedKind, normalizedPostID); err != nil {
		return err
	}

	return nil
}

func (r *alarmStateRepository) FindAlarmStateByPostID(ctx context.Context, kind domain.OutboxKind, postID string) (*domain.YouTubeCommunityShortsAlarmState, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("find alarm state by post id: db is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(kind, postID)
	if err != nil {
		return nil, fmt.Errorf("find alarm state by post id: %w", err)
	}

	var row domain.YouTubeCommunityShortsAlarmState
	found, err := dbx.GetSQL(ctx, r.db, &row, "find alarm state by post id: query row", `
		SELECT kind, post_id, content_id, channel_id, actual_published_at, detected_at,
		       published_at_retry_after, authorized_at, alarm_sent_at, delivery_status, created_at, updated_at
		FROM youtube_community_shorts_alarm_states
		WHERE kind = ? AND post_id = ?
		LIMIT 1
	`, normalizedKind, normalizedPostID)
	if err != nil {
		return nil, err
	}
	if !found {
		return nil, nil
	}

	return &row, nil
}

func hasPublishedAtRetryAfterColumn(db trackingDB) bool {
	if db == nil {
		return false
	}
	var exists bool
	err := db.QueryRow(context.Background(), `
		SELECT EXISTS (
			SELECT 1
			FROM information_schema.columns
			WHERE table_name = 'youtube_community_shorts_alarm_states'
			  AND column_name = 'published_at_retry_after'
		)
	`).Scan(&exists)
	return err == nil && exists
}

func (r *alarmStateRepository) requirePublishedAtRetryAfterColumn(action string) error {
	if r == nil || r.hasPublishedAtRetryAfter {
		return nil
	}
	return fmt.Errorf("%s: missing required column published_at_retry_after", action)
}
