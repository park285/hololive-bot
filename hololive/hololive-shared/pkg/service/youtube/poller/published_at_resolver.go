package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/youtube/scraper"
	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

type PendingPublishedAtResolver struct {
	db                *gorm.DB
	client            *scraper.Client
	routeDecider      NotificationRouteDecider
	interval          time.Duration
	batchSize         int
	maxResolvePerRun  int
	minDetectedAge    time.Duration
	failureBackoffTTL time.Duration
	softLimiter       *scraper.RateLimiter
	backoffStore      pendingPublishedAtBackoffStore
	logger            *slog.Logger
}

func NewPendingPublishedAtResolver(
	db *gorm.DB,
	client *scraper.Client,
	routeDecider NotificationRouteDecider,
	interval time.Duration,
	batchSize int,
	logger *slog.Logger,
) *PendingPublishedAtResolver {
	return NewPendingPublishedAtResolverWithControls(
		db,
		client,
		routeDecider,
		interval,
		batchSize,
		batchSize,
		20*time.Second,
		5*time.Minute,
		nil,
		nil,
		logger,
	)
}

func NewPendingPublishedAtResolverWithControls(
	db *gorm.DB,
	client *scraper.Client,
	routeDecider NotificationRouteDecider,
	interval time.Duration,
	batchSize int,
	maxResolvePerRun int,
	minDetectedAge time.Duration,
	failureBackoffTTL time.Duration,
	softLimiter *scraper.RateLimiter,
	backoffStore pendingPublishedAtBackoffStore,
	logger *slog.Logger,
) *PendingPublishedAtResolver {
	if interval <= 0 {
		interval = 15 * time.Second
	}
	if batchSize <= 0 {
		batchSize = 50
	}
	if maxResolvePerRun <= 0 {
		maxResolvePerRun = batchSize
	}
	if minDetectedAge <= 0 {
		minDetectedAge = 20 * time.Second
	}
	if failureBackoffTTL <= 0 {
		failureBackoffTTL = 5 * time.Minute
	}
	if logger == nil {
		logger = slog.Default()
	}

	return &PendingPublishedAtResolver{
		db:                db,
		client:            client,
		routeDecider:      routeDecider,
		interval:          interval,
		batchSize:         batchSize,
		maxResolvePerRun:  maxResolvePerRun,
		minDetectedAge:    minDetectedAge,
		failureBackoffTTL: failureBackoffTTL,
		softLimiter:       softLimiter,
		backoffStore:      backoffStore,
		logger:            logger,
	}
}

func (r *PendingPublishedAtResolver) Start(ctx context.Context) {
	if r == nil || r.db == nil || r.client == nil {
		return
	}

	ticker := time.NewTicker(r.resolverInterval())
	defer ticker.Stop()

	for {
		detectedBefore := time.Now().Add(-r.resolverMinDetectedAge())
		if err := r.runOnce(ctx, detectedBefore); err != nil && ctx.Err() == nil {
			r.logger.Warn("Pending published_at resolver iteration failed",
				slog.Any("error", err),
			)
		}

		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
		}
	}
}

func (r *PendingPublishedAtResolver) runOnce(ctx context.Context, detectedBefore time.Time) error {
	if r == nil || r.db == nil {
		return fmt.Errorf("run pending published_at resolver: db is nil")
	}
	if r.client == nil {
		return fmt.Errorf("run pending published_at resolver: client is nil")
	}

	repo := newPublishedAtResolverRepository(r.db)
	tracking := trackingrepo.NewRepository(r.db)
	batchSize := r.resolverBatchSize()
	maxResolvePerRun := r.resolverMaxResolvePerRun()
	failureBackoffTTL := r.resolverFailureBackoffTTL()
	processed := 0
	var cursor *trackingrepo.PublishedAtResolutionCursor
	for processed < maxResolvePerRun {
		candidates, nextCursor, err := tracking.ListPendingPublishedAtResolutionsPage(
			ctx,
			detectedBefore,
			cursor,
			minInt(batchSize, maxResolvePerRun-processed),
		)
		if err != nil {
			return fmt.Errorf("run pending published_at resolver: list candidates: %w", err)
		}
		setPublishedAtResolverPendingCandidates(len(candidates))
		if len(candidates) == 0 {
			return nil
		}

		for i := range candidates {
			select {
			case <-ctx.Done():
				return ctx.Err()
			default:
			}

			candidate := candidates[i]

			if r.backoffStore != nil {
				active, err := r.backoffStore.Active(ctx, candidate)
				if err != nil {
					r.logger.Warn("Pending published_at resolver failed to read backoff state",
						slog.String("kind", string(candidate.Kind)),
						slog.String("post_id", candidate.PostID),
						slog.String("content_id", candidate.ContentID),
						slog.Any("error", err),
					)
				}
				if active {
					observePublishedAtResolverSkipped(candidate.Kind, "backoff_active")
					continue
				}
			}

			if r.softLimiter != nil {
				if err := r.softLimiter.Wait(ctx); err != nil {
					return fmt.Errorf("run pending published_at resolver: wait soft limiter: %w", err)
				}
			}

			processed++
			observePublishedAtResolutionAttempt(candidate.Kind)
			publishedAt, err := r.resolveCandidatePublishedAt(ctx, candidate)
			if err != nil {
				observePublishedAtResolutionFailure(candidate.Kind)
				if r.backoffStore != nil {
					_ = r.backoffStore.Mark(ctx, candidate, failureBackoffTTL)
				}
				r.logger.Warn("Pending published_at resolver failed to resolve candidate",
					slog.String("kind", string(candidate.Kind)),
					slog.String("post_id", candidate.PostID),
					slog.String("content_id", candidate.ContentID),
					slog.Any("error", err),
				)
				continue
			}
			if publishedAt == nil || publishedAt.IsZero() {
				if r.backoffStore != nil {
					_ = r.backoffStore.Mark(ctx, candidate, failureBackoffTTL)
				}
				observePublishedAtResolverSkipped(candidate.Kind, "published_at_empty")
				continue
			}
			if r.backoffStore != nil {
				_ = r.backoffStore.Clear(ctx, candidate)
			}
			observePublishedAtResolutionSuccess(candidate.Kind)

			result, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
			if err != nil {
				r.logger.Warn("Pending published_at resolver failed to finalize candidate",
					slog.String("kind", string(candidate.Kind)),
					slog.String("post_id", candidate.PostID),
					slog.String("content_id", candidate.ContentID),
					slog.Any("error", err),
				)
				continue
			}
			if result.enqueued {
				observePublishedAtResolverEnqueued(candidate.Kind)
				r.logger.Info("published_at_resolver_enqueued",
					slog.String("kind", string(candidate.Kind)),
					slog.String("post_id", candidate.PostID),
					slog.String("channel_id", candidate.ChannelID),
					slog.String("published_at", yttimestamp.Format(*publishedAt)),
					slog.String("reason", result.reason),
				)
				continue
			}
			observePublishedAtResolverSkipped(candidate.Kind, result.reason)
			r.logger.Info("published_at_resolver_enqueue_skipped",
				slog.String("kind", string(candidate.Kind)),
				slog.String("post_id", candidate.PostID),
				slog.String("channel_id", candidate.ChannelID),
				slog.String("published_at", yttimestamp.Format(*publishedAt)),
				slog.String("reason", result.reason),
			)
		}

		if nextCursor == nil {
			return nil
		}
		cursor = nextCursor
	}

	return nil
}

func pendingPublishedAtCandidateKey(candidate trackingrepo.PublishedAtResolutionCandidate) string {
	return string(candidate.Kind) + "\x00" + candidate.PostID
}

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

func (r *PendingPublishedAtResolver) resolveCandidatePublishedAt(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
) (*time.Time, error) {
	switch candidate.Kind {
	case domain.OutboxKindNewShort:
		videoID := normalizeShortVideoResourceID(candidate.PostID)
		if videoID == "" {
			videoID = normalizeShortVideoResourceID(candidate.ContentID)
		}
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
	case domain.OutboxKindCommunityPost:
		postID := normalizeCommunityResourceID(candidate.PostID)
		if postID == "" {
			postID = normalizeCommunityResourceID(candidate.ContentID)
		}
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
	default:
		return nil, fmt.Errorf("resolve candidate published_at: unsupported kind %s", candidate.Kind)
	}
}
