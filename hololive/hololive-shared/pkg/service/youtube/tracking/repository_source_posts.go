package tracking

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *GormRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	if record == nil {
		return fmt.Errorf("upsert source post: record is nil")
	}
	return r.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{record})
}

func (r *GormRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	if len(records) == 0 {
		return nil
	}
	if r == nil || r.db == nil {
		return fmt.Errorf("upsert source posts batch: db is nil")
	}

	normalized, err := normalizeSourcePostsBatch(records)
	if err != nil {
		return fmt.Errorf("upsert source posts batch: %w", err)
	}

	query, args := buildSourcePostsBatchUpsert(normalized, yttimestamp.Normalize(time.Now()))
	if err := r.db.WithContext(ctx).Exec(query, args...).Error; err != nil {
		return fmt.Errorf("upsert source posts batch: exec query: %w", err)
	}

	return nil
}

func normalizeSourcePostsBatch(
	records []*domain.YouTubeCommunityShortsSourcePost,
) ([]*domain.YouTubeCommunityShortsSourcePost, error) {
	normalized := make([]*domain.YouTubeCommunityShortsSourcePost, 0, len(records))
	for i, record := range records {
		normalizedRecord, err := normalizeSourcePost(record)
		if err != nil {
			return nil, fmt.Errorf("normalize record at index %d: %w", i, err)
		}
		normalized = append(normalized, normalizedRecord)
	}
	return normalized, nil
}

func buildSourcePostsBatchUpsert(
	normalized []*domain.YouTubeCommunityShortsSourcePost,
	now time.Time,
) (string, []any) {
	args := make([]any, 0, len(normalized)*7)
	var sb strings.Builder
	sb.WriteString(`
		INSERT INTO youtube_community_shorts_source_posts
			(kind, post_id, channel_id, actual_published_at, detected_at, created_at, updated_at)
		VALUES
	`)

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}
		sb.WriteString("(?, ?, ?, ?, ?, ?, ?)")
		args = append(args, record.Kind, record.PostID, record.ChannelID, record.ActualPublishedAt, record.DetectedAt, now, now)
	}

	sb.WriteString(`
		ON CONFLICT (kind, post_id) DO UPDATE
		SET channel_id = EXCLUDED.channel_id,
		    actual_published_at = CASE
		        WHEN EXCLUDED.actual_published_at IS NULL THEN youtube_community_shorts_source_posts.actual_published_at
		        ELSE EXCLUDED.actual_published_at
		    END,
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_source_posts.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_source_posts.detected_at
		    END,
		    updated_at = EXCLUDED.updated_at
	`)

	return sb.String(), args
}

func (r *GormRepository) ListSourcePostsDetectedWithinWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list source posts within detected window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list source posts within detected window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list source posts within detected window: window end is empty")
	}

	startUTC := yttimestamp.Normalize(windowStart)
	endUTC := yttimestamp.Normalize(windowEnd)
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within detected window: window start must be before window end")
	}

	var rows []domain.YouTubeCommunityShortsSourcePost
	if err := r.db.WithContext(ctx).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("detected_at >= ?", startUTC).
		Where("detected_at < ?", endUTC).
		Order("detected_at DESC").
		Order("post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list source posts within detected window: query rows: %w", err)
	}

	return rows, nil
}

func (r *GormRepository) ListSourcePostsWithinObservationWindow(
	ctx context.Context,
	windowStart time.Time,
	windowEnd time.Time,
	detectedBefore time.Time,
) ([]domain.YouTubeCommunityShortsSourcePost, error) {
	if r == nil || r.db == nil {
		return nil, fmt.Errorf("list source posts within observation window: db is nil")
	}
	if windowStart.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: window start is empty")
	}
	if windowEnd.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: window end is empty")
	}
	if detectedBefore.IsZero() {
		return nil, fmt.Errorf("list source posts within observation window: detected before is empty")
	}

	startUTC := yttimestamp.Normalize(windowStart)
	endUTC := yttimestamp.Normalize(windowEnd)
	detectedBeforeUTC := yttimestamp.Normalize(detectedBefore)
	if !startUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within observation window: window start must be before window end")
	}
	if detectedBeforeUTC.Before(endUTC) {
		return nil, fmt.Errorf("list source posts within observation window: detected before must be on or after window end")
	}

	var rows []domain.YouTubeCommunityShortsSourcePost
	if err := r.db.WithContext(ctx).
		Where("kind IN ?", []domain.OutboxKind{domain.OutboxKindCommunityPost, domain.OutboxKindNewShort}).
		Where("COALESCE(actual_published_at, detected_at) >= ?", startUTC).
		Where("COALESCE(actual_published_at, detected_at) < ?", endUTC).
		Where("detected_at < ?", detectedBeforeUTC).
		Order("COALESCE(actual_published_at, detected_at) DESC").
		Order("detected_at DESC").
		Order("post_id ASC").
		Find(&rows).Error; err != nil {
		return nil, fmt.Errorf("list source posts within observation window: query rows: %w", err)
	}

	return rows, nil
}

func normalizeSourcePost(record *domain.YouTubeCommunityShortsSourcePost) (*domain.YouTubeCommunityShortsSourcePost, error) {
	if record == nil {
		return nil, fmt.Errorf("record is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(record.Kind, record.PostID)
	if err != nil {
		return nil, err
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, fmt.Errorf("channel id is empty")
	}
	if record.DetectedAt.IsZero() {
		return nil, fmt.Errorf("detected_at is empty")
	}

	return &domain.YouTubeCommunityShortsSourcePost{
		Kind:              normalizedKind,
		PostID:            normalizedPostID,
		ChannelID:         normalizedChannelID,
		ActualPublishedAt: yttimestamp.NormalizePtr(record.ActualPublishedAt),
		DetectedAt:        yttimestamp.Normalize(record.DetectedAt),
	}, nil
}

func normalizeSourcePostIdentity(kind domain.OutboxKind, postID string) (domain.OutboxKind, string, error) {
	normalizedKind, normalizedPostID, err := normalizeIdentity(kind, postID)
	if err != nil {
		return "", "", err
	}

	canonicalPostID, err := ytcontentid.ForOutboxKind(normalizedKind, normalizedPostID)
	if err == nil && strings.TrimSpace(canonicalPostID) != "" {
		return normalizedKind, canonicalPostID, nil
	}
	return normalizedKind, strings.TrimSpace(normalizedPostID), nil
}
