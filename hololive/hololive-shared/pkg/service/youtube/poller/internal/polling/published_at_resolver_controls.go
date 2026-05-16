package polling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func minInt(left, right int) int {
	if left < right {
		return left
	}
	return right
}

func (r *PendingPublishedAtResolver) resolverInterval() time.Duration {
	if r == nil || r.interval <= 0 {
		return 15 * time.Second
	}
	return r.interval
}

func (r *PendingPublishedAtResolver) resolverBatchSize() int {
	if r == nil || r.batchSize <= 0 {
		return 50
	}
	return r.batchSize
}

func (r *PendingPublishedAtResolver) resolverMaxResolvePerRun() int {
	if r != nil && r.maxResolvePerRun > 0 {
		return r.maxResolvePerRun
	}
	batchSize := r.resolverBatchSize()
	if batchSize < 20 {
		return 20
	}
	return batchSize
}

func (r *PendingPublishedAtResolver) resolverMaxRunDuration() time.Duration {
	if r == nil || r.maxRunDuration <= 0 {
		return 12 * time.Second
	}
	return r.maxRunDuration
}

func (r *PendingPublishedAtResolver) resolverResolveTimeout() time.Duration {
	if r == nil || r.resolveTimeout <= 0 {
		return 10 * time.Second
	}
	return r.resolveTimeout
}

func (r *PendingPublishedAtResolver) resolverMinDetectedAge() time.Duration {
	if r == nil || r.minDetectedAge <= 0 {
		return 20 * time.Second
	}
	return r.minDetectedAge
}

func (r *PendingPublishedAtResolver) resolverFailureBackoffTTL() time.Duration {
	if r == nil || r.failureBackoffTTL <= 0 {
		return 5 * time.Minute
	}
	return r.failureBackoffTTL
}

func (r *PendingPublishedAtResolver) markPublishedAtRetryAfter(
	tracking *trackingrepo.GormRepository,
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	retryAfter time.Time,
	forceLive bool,
) error {
	if tracking == nil {
		return fmt.Errorf("mark published_at retry after: tracking repository is nil")
	}
	if !forceLive {
		if err := tracking.MarkPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID, retryAfter); err != nil {
			return fmt.Errorf("mark published_at retry after: %w", err)
		}
		return nil
	}

	retryTTL := r.resolverFailureBackoffTTL()
	if retryTTL <= 0 || retryTTL > time.Second {
		retryTTL = time.Second
	}
	retryCtx, cancel := context.WithTimeout(context.Background(), retryTTL)
	defer cancel()

	if err := tracking.MarkPublishedAtRetryAfter(retryCtx, candidate.Kind, candidate.PostID, retryAfter); err != nil {
		return fmt.Errorf("mark published_at retry after: %w", err)
	}

	return nil
}

func (r *PendingPublishedAtResolver) markPublishedAtRetryAfterWithReporting(
	tracking *trackingrepo.GormRepository,
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	retryAfter time.Time,
	forceLive bool,
	reason string,
) {
	if err := r.markPublishedAtRetryAfter(tracking, ctx, candidate, retryAfter, forceLive); err != nil {
		observePublishedAtResolverSkipped(candidate.Kind, "retry_after_write_failed")
		r.logger.Warn("Pending published_at resolver failed to write retry_after",
			slog.String("kind", string(candidate.Kind)),
			slog.String("post_id", candidate.PostID),
			slog.String("content_id", candidate.ContentID),
			slog.String("reason", reason),
			slog.Bool("force_live", forceLive),
			slog.Any("error", err),
		)
	}
}

func (r *PendingPublishedAtResolver) resolveCandidatePublishedAt(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (*time.Time, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		return r.resolveShortCandidatePublishedAt(ctx, candidate)
	case domain.OutboxKindCommunityPost:
		return r.resolveCommunityCandidatePublishedAt(ctx, candidate)
	default:
		return nil, fmt.Errorf("resolve candidate published_at: unsupported kind %s", candidate.Kind)
	}
}

func (r *PendingPublishedAtResolver) resolveShortCandidatePublishedAt(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (*time.Time, error) {
	videoID := normalizeCandidateResourceID(
		candidate,
		normalizeShortVideoResourceID,
	)
	if videoID == "" {
		return nil, fmt.Errorf("resolve candidate published_at: empty short video id")
	}
	publishedAt, err := r.client.ResolveVideoPublishedAt(ctx, videoID)
	if err != nil {
		if errors.Is(err, scraper.ErrPublishedAtNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve candidate published_at: resolve short video %s: %w", videoID, err)
	}
	return yttimestamp.NormalizePtr(publishedAt), nil
}

func (r *PendingPublishedAtResolver) resolveCommunityCandidatePublishedAt(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (*time.Time, error) {
	postID := normalizeCandidateResourceID(
		candidate,
		normalizeCommunityResourceID,
	)
	if postID == "" {
		return nil, fmt.Errorf("resolve candidate published_at: empty community post id")
	}
	publishedAt, err := r.client.ResolveCommunityPostPublishedAt(ctx, postID)
	if err != nil {
		if errors.Is(err, scraper.ErrCommunityPublishedAtNotFound) {
			return nil, nil
		}
		return nil, fmt.Errorf("resolve candidate published_at: resolve community post %s: %w", postID, err)
	}
	return yttimestamp.NormalizePtr(publishedAt), nil
}

func normalizeCandidateResourceID(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	normalize func(string) string,
) string {
	id := normalize(candidate.PostID)
	if id != "" {
		return id
	}
	return normalize(candidate.ContentID)
}
