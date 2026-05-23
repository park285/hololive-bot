package observation

import (
	"context"
	"fmt"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

type pendingResolutionRow struct {
	PriorityBucket        int               `gorm:"column:priority_bucket"`
	Kind                  domain.OutboxKind `gorm:"column:kind"`
	PostID                string            `gorm:"column:post_id"`
	ContentID             string            `gorm:"column:content_id"`
	ChannelID             string            `gorm:"column:channel_id"`
	DetectedAt            time.Time         `gorm:"column:detected_at"`
	PublishedAtRetryAfter *time.Time        `gorm:"column:published_at_retry_after"`
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

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Updates(map[string]any{
			"published_at_retry_after": yttimestamp.Normalize(retryAfter),
			"updated_at":               yttimestamp.Normalize(time.Now()),
		})
	if result.Error != nil {
		return fmt.Errorf("mark published_at retry after: %w", result.Error)
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

	result := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Updates(map[string]any{
			"published_at_retry_after": nil,
			"updated_at":               yttimestamp.Normalize(time.Now()),
		})
	if result.Error != nil {
		return fmt.Errorf("clear published_at retry after: %w", result.Error)
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
	result := r.db.WithContext(ctx).
		Where("kind = ? AND post_id = ?", normalizedKind, normalizedPostID).
		Limit(1).
		Find(&row)
	if result.Error != nil {
		return nil, fmt.Errorf("find alarm state by post id: query row: %w", result.Error)
	}
	if result.RowsAffected == 0 {
		return nil, nil
	}

	return &row, nil
}

func hasPublishedAtRetryAfterColumn(db *gorm.DB) bool {
	if db == nil || db.Migrator() == nil {
		return false
	}
	return db.Migrator().HasColumn(&domain.YouTubeCommunityShortsAlarmState{}, "published_at_retry_after")
}

func (r *alarmStateRepository) requirePublishedAtRetryAfterColumn(action string) error {
	if r == nil || r.hasPublishedAtRetryAfter {
		return nil
	}
	return fmt.Errorf("%s: missing required column published_at_retry_after", action)
}
