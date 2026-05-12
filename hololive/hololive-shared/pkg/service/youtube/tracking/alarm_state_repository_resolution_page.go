package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func validatePendingPublishedAtResolutionPageRequest(r *GormRepository, detectedBefore, referenceNow time.Time, limit int) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("list pending published_at resolutions page: db is nil")
	}
	if detectedBefore.IsZero() {
		return fmt.Errorf("list pending published_at resolutions page: detected before is empty")
	}
	if referenceNow.IsZero() {
		return fmt.Errorf("list pending published_at resolutions page: reference now is empty")
	}
	if limit <= 0 {
		return fmt.Errorf("list pending published_at resolutions page: limit must be positive")
	}
	return nil
}

func (r *GormRepository) loadPendingPublishedAtResolutionRows(
	ctx context.Context,
	referenceNow time.Time,
	detectedBefore time.Time,
	cursor *PublishedAtResolutionCursor,
	limit int,
) ([]pendingResolutionRow, error) {
	if err := r.requirePublishedAtRetryAfterColumn("list pending published_at resolutions page"); err != nil {
		return nil, err
	}

	var rows []pendingResolutionRow
	query := r.db.WithContext(ctx).
		Model(&domain.YouTubeCommunityShortsAlarmState{}).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("actual_published_at IS NULL").
		Where("detected_at < ?", yttimestamp.Normalize(detectedBefore)).
		Select(`CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END AS priority_bucket,
			kind, post_id, content_id, channel_id, detected_at, published_at_retry_after`).
		Where("(published_at_retry_after IS NULL OR published_at_retry_after <= ?)", yttimestamp.Normalize(referenceNow))
	if cursor != nil {
		query = query.Where(
			`(CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END > ?)
			OR (CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END = ? AND detected_at > ?)
			OR (CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END = ? AND detected_at = ? AND post_id > ?)`,
			cursor.PriorityBucket,
			cursor.PriorityBucket,
			yttimestamp.Normalize(cursor.DetectedAt),
			cursor.PriorityBucket,
			yttimestamp.Normalize(cursor.DetectedAt),
			strings.TrimSpace(cursor.PostID),
		)
	}
	if err := query.
		Order("priority_bucket ASC").
		Order("detected_at ASC").
		Order("post_id ASC").
		Limit(limit).
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list pending published_at resolutions page: query rows: %w", err)
	}

	return rows, nil
}

func buildPublishedAtResolutionCandidates(rows []pendingResolutionRow) ([]PublishedAtResolutionCandidate, error) {
	candidates := make([]PublishedAtResolutionCandidate, 0, len(rows))
	for i := range rows {
		candidate, err := buildPublishedAtResolutionCandidate(rows[i])
		if err != nil {
			return nil, fmt.Errorf("list pending published_at resolutions page: row %d: %w", i, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func buildPublishedAtResolutionCandidate(row pendingResolutionRow) (PublishedAtResolutionCandidate, error) {
	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(row.Kind, row.PostID)
	if err != nil {
		return PublishedAtResolutionCandidate{}, fmt.Errorf("normalize post id: %w", err)
	}
	normalizedContentKind, normalizedContentID, err := normalizeIdentity(row.Kind, row.ContentID)
	if err != nil {
		return PublishedAtResolutionCandidate{}, fmt.Errorf("normalize content id: %w", err)
	}
	if normalizedKind != normalizedContentKind {
		return PublishedAtResolutionCandidate{}, fmt.Errorf("kind mismatch")
	}

	return PublishedAtResolutionCandidate{
		Kind:       normalizedKind,
		PostID:     normalizedPostID,
		ContentID:  canonicalTrackingIdentity(normalizedKind, normalizedContentID),
		ChannelID:  strings.TrimSpace(row.ChannelID),
		DetectedAt: yttimestamp.Normalize(row.DetectedAt),
	}, nil
}

func buildPendingPublishedAtResolutionCursor(
	rows []pendingResolutionRow,
	candidates []PublishedAtResolutionCandidate,
	limit int,
) *PublishedAtResolutionCursor {
	if len(candidates) == 0 || len(candidates) < limit {
		return nil
	}
	last := candidates[len(candidates)-1]
	return &PublishedAtResolutionCursor{
		PriorityBucket: rows[len(rows)-1].PriorityBucket,
		DetectedAt:     last.DetectedAt,
		PostID:         last.PostID,
	}
}
