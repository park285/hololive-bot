package observation

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/dbx"
	"github.com/kapu/hololive-shared/pkg/domain"
	ytcontentid "github.com/kapu/hololive-shared/pkg/service/youtube/contentid"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
)

func (r *sourcePostRepository) UpsertSourcePost(ctx context.Context, record *domain.YouTubeCommunityShortsSourcePost) error {
	if record == nil {
		return errors.New("upsert source post: record is nil")
	}

	if err := r.UpsertSourcePostsBatch(ctx, []*domain.YouTubeCommunityShortsSourcePost{record}); err != nil {
		return fmt.Errorf("upsert source posts batch: %w", err)
	}

	return nil
}

func (r *sourcePostRepository) UpsertSourcePostsBatch(ctx context.Context, records []*domain.YouTubeCommunityShortsSourcePost) error {
	if len(records) == 0 {
		return nil
	}

	if r == nil || r.db == nil {
		return errors.New("upsert source posts batch: db is nil")
	}

	normalized, err := normalizeSourcePostsBatch(records)
	if err != nil {
		return fmt.Errorf("upsert source posts batch: %w", err)
	}

	query, args := buildSourcePostsBatchUpsert(normalized, yttimestamp.Normalize(time.Now()))
	if _, err := dbx.ExecSQL(ctx, r.db, "upsert source posts batch: exec query", query, args...); err != nil {
		return fmt.Errorf("exec SQL: %w", err)
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

	sb.WriteString(mustSQL("repository_source_posts_0063_01.sql"))

	for i, record := range normalized {
		if i > 0 {
			sb.WriteByte(',')
		}

		sb.WriteString("(?, ?, ?, ?, ?, ?, ?)")

		args = append(args, record.Kind, record.PostID, record.ChannelID, record.ActualPublishedAt, record.DetectedAt, now, now)
	}

	sb.WriteString(mustSQL("repository_source_posts_0077_02.sql"))

	return sb.String(), args
}

func normalizeSourcePost(record *domain.YouTubeCommunityShortsSourcePost) (*domain.YouTubeCommunityShortsSourcePost, error) {
	if record == nil {
		return nil, errors.New("record is nil")
	}

	normalizedKind, normalizedPostID, err := normalizeSourcePostIdentity(record.Kind, record.PostID)
	if err != nil {
		return nil, fmt.Errorf("normalize source post identity: %w", err)
	}

	normalizedChannelID := strings.TrimSpace(record.ChannelID)
	if normalizedChannelID == "" {
		return nil, errors.New("channel id is empty")
	}

	if record.DetectedAt.IsZero() {
		return nil, errors.New("detected_at is empty")
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
		return "", "", fmt.Errorf("normalize identity: %w", err)
	}

	canonicalPostID, err := ytcontentid.ForOutboxKind(normalizedKind, normalizedPostID)
	if err != nil {
		return "", "", fmt.Errorf("canonicalize source post identity: %w", err)
	}

	return normalizedKind, canonicalPostID, nil
}
