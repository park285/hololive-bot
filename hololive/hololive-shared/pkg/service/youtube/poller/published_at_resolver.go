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
	maxRunDuration    time.Duration
	resolveTimeout    time.Duration
	minDetectedAge    time.Duration
	failureBackoffTTL time.Duration
	logger            *slog.Logger
}

var errResolverParentCanceled = errors.New("pending published_at resolver parent context canceled")

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
		12*time.Second,
		10*time.Second,
		20*time.Second,
		5*time.Minute,
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
	maxRunDuration time.Duration,
	resolveTimeout time.Duration,
	minDetectedAge time.Duration,
	failureBackoffTTL time.Duration,
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
	if maxRunDuration <= 0 {
		maxRunDuration = 12 * time.Second
	}
	if resolveTimeout <= 0 {
		resolveTimeout = 10 * time.Second
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
		maxRunDuration:    maxRunDuration,
		resolveTimeout:    resolveTimeout,
		minDetectedAge:    minDetectedAge,
		failureBackoffTTL: failureBackoffTTL,
		logger:            logger,
	}
}

func (r *PendingPublishedAtResolver) Start(ctx context.Context) {
	if r == nil || r.db == nil || r.client == nil {
		return
	}

	for {
		detectedBefore := time.Now().Add(-r.resolverMinDetectedAge())
		if err := r.runOnce(ctx, detectedBefore); err != nil && ctx.Err() == nil {
			r.logger.Warn("Pending published_at resolver iteration failed",
				slog.Any("error", err),
			)
		}

		timer := time.NewTimer(r.resolverInterval())
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				select {
				case <-timer.C:
				default:
				}
			}
			return
		case <-timer.C:
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
	runDeadline := time.Now().Add(r.resolverMaxRunDuration())
	resolveTimeout := r.resolverResolveTimeout()
	failureBackoffTTL := r.resolverFailureBackoffTTL()
	processed := 0
	var cursor *trackingrepo.PublishedAtResolutionCursor
	for processed < maxResolvePerRun {
		if time.Now().After(runDeadline) {
			return nil
		}
		now := time.Now()
		candidates, nextCursor, err := tracking.ListPendingPublishedAtResolutionsPage(
			ctx,
			now,
			detectedBefore,
			cursor,
			minInt(batchSize, maxResolvePerRun-processed),
		)
		if err != nil {
			return fmt.Errorf("run pending published_at resolver: list candidates: %w", err)
		}
		setPublishedAtResolverPageCandidates(len(candidates))
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
			observePublishedAtResolverScanned(candidate.Kind)
			if time.Now().After(runDeadline) {
				return nil
			}
			if time.Until(runDeadline) < resolveTimeout {
				observePublishedAtResolverSkipped(candidate.Kind, "run_budget_exhausted")
				return nil
			}

			processed++
			observePublishedAtResolutionAttempt(candidate.Kind)
			publishedAt, err := r.resolveCandidateWithTimeout(ctx, candidate, resolveTimeout)
			if err != nil {
				if errors.Is(err, errResolverParentCanceled) {
					return ctx.Err()
				}

				observePublishedAtResolutionFailure(candidate.Kind)
				isResolveTimeout := errors.Is(err, context.DeadlineExceeded)
				r.markPublishedAtRetryAfterWithReporting(
					tracking,
					ctx,
					candidate,
					time.Now().Add(failureBackoffTTL),
					isResolveTimeout,
					"resolve_failed",
				)
				if isResolveTimeout {
					observePublishedAtResolverSkipped(candidate.Kind, "resolve_timeout")
				}
				r.logger.Warn("Pending published_at resolver failed to resolve candidate",
					slog.String("kind", string(candidate.Kind)),
					slog.String("post_id", candidate.PostID),
					slog.String("content_id", candidate.ContentID),
					slog.Duration("resolve_timeout", resolveTimeout),
					slog.Any("error", err),
				)
				continue
			}
			if publishedAt == nil || publishedAt.IsZero() {
				r.markPublishedAtRetryAfterWithReporting(
					tracking,
					ctx,
					candidate,
					time.Now().Add(failureBackoffTTL),
					false,
					"published_at_empty",
				)
				observePublishedAtResolverSkipped(candidate.Kind, "published_at_empty")
				continue
			}
			observePublishedAtResolutionSuccess(candidate.Kind)

			result, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
			if err != nil {
				r.markPublishedAtRetryAfterWithReporting(
					tracking,
					ctx,
					candidate,
					time.Now().Add(failureBackoffTTL),
					false,
					"finalize_failed",
				)
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

func (r *PendingPublishedAtResolver) resolveCandidateWithTimeout(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	resolveTimeout time.Duration,
) (*time.Time, error) {
	resolveCtx, cancel := context.WithTimeout(context.WithoutCancel(ctx), resolveTimeout)
	stopRelay := context.AfterFunc(ctx, cancel)
	defer func() {
		stopRelay()
		cancel()
	}()

	publishedAt, err := r.resolveCandidatePublishedAt(resolveCtx, candidate)
	if err != nil && errors.Is(err, context.Canceled) && ctx.Err() != nil {
		return nil, errResolverParentCanceled
	}

	return publishedAt, err
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
		return tracking.MarkPublishedAtRetryAfter(ctx, candidate.Kind, candidate.PostID, retryAfter)
	}

	retryTTL := r.resolverFailureBackoffTTL()
	if retryTTL <= 0 || retryTTL > time.Second {
		retryTTL = time.Second
	}
	retryCtx, cancel := context.WithTimeout(context.Background(), retryTTL)
	defer cancel()

	return tracking.MarkPublishedAtRetryAfter(retryCtx, candidate.Kind, candidate.PostID, retryAfter)
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
