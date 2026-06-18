package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func validatePendingPublishedAtResolutionPageRequest(r *alarmStateRepository, detectedBefore, referenceNow time.Time, limit int) error {
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

func (r *alarmStateRepository) loadPendingPublishedAtResolutionRows(
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
	query := `
		SELECT CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END AS priority_bucket,
		       kind, post_id, content_id, channel_id, detected_at, published_at_retry_after
		FROM youtube_community_shorts_alarm_states
		WHERE kind IN (?, ?)
		  AND actual_published_at IS NULL
		  AND detected_at < ?
		  AND (published_at_retry_after IS NULL OR published_at_retry_after <= ?)
	`
	args := []any{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort, yttimestamp.Normalize(detectedBefore), yttimestamp.Normalize(referenceNow)}
	if cursor != nil {
		query += `
		  AND ((CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END > ?)
			OR (CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END = ? AND detected_at > ?)
			OR (CASE WHEN authorized_at IS NULL AND alarm_sent_at IS NULL THEN 0 ELSE 1 END = ? AND detected_at = ? AND post_id > ?))
		`
		args = append(args,
			cursor.PriorityBucket,
			cursor.PriorityBucket,
			yttimestamp.Normalize(cursor.DetectedAt),
			cursor.PriorityBucket,
			yttimestamp.Normalize(cursor.DetectedAt),
			strings.TrimSpace(cursor.PostID),
		)
	}
	query += `
		ORDER BY priority_bucket ASC, detected_at ASC, post_id ASC
		LIMIT ?
	`
	args = append(args, limit)
	if err := dbx.SelectSQL(ctx, r.db, &rows, "list pending published_at resolutions page: query rows", query, args...); err != nil {
		return nil, err
	}

	return rows, nil
}

func buildPublishedAtResolutionCandidates(rows []pendingResolutionRow) ([]PublishedAtResolutionCandidate, error) {
	candidates := make([]PublishedAtResolutionCandidate, 0, len(rows))
	for i := range rows {
		candidate, err := buildPublishedAtResolutionCandidate(&rows[i])
		if err != nil {
			return nil, fmt.Errorf("list pending published_at resolutions page: row %d: %w", i, err)
		}
		candidates = append(candidates, candidate)
	}
	return candidates, nil
}

func buildPublishedAtResolutionCandidate(row *pendingResolutionRow) (PublishedAtResolutionCandidate, error) {
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
	if len(rows) == 0 {
		return nil
	}
	last := candidates[len(candidates)-1]
	return &PublishedAtResolutionCursor{
		PriorityBucket: rows[len(rows)-1].PriorityBucket,
		DetectedAt:     last.DetectedAt,
		PostID:         last.PostID,
	}
}
