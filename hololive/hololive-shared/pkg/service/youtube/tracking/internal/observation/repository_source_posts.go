package observation

import (
	"context"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/internal/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *sourcePostRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	if record == nil {
		return fmt.Errorf("upsert source post: record is nil")
	}
	return r.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{record})
}

func (r *sourcePostRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
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
	if _, err := dbx.ExecSQL(ctx, r.db, "upsert source posts batch: exec query", query, args...); err != nil {
		return err
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
) (result1 string, result2 []any) {
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
		    actual_published_at = COALESCE(youtube_community_shorts_source_posts.actual_published_at, EXCLUDED.actual_published_at),
		    detected_at = CASE
		        WHEN EXCLUDED.detected_at < youtube_community_shorts_source_posts.detected_at THEN EXCLUDED.detected_at
		        ELSE youtube_community_shorts_source_posts.detected_at
		    END,
		    updated_at = EXCLUDED.updated_at
	`)

	return sb.String(), args
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
