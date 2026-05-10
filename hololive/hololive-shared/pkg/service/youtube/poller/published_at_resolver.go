package poller

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	"gorm.io/gorm"

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

type publishedAtResolverCandidateResult struct {
	processed int
	stop      bool
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
		if err := r.RunOnce(ctx); err != nil && ctx.Err() == nil {
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

func (r *PendingPublishedAtResolver) RunOnce(ctx context.Context) error {
	if r == nil {
		return fmt.Errorf("run pending published_at resolver: resolver is nil")
	}

	detectedBefore := time.Now().Add(-r.resolverMinDetectedAge())
	return r.runOnce(ctx, detectedBefore)
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
			return r.recoverResolvedPublishedAtDispatchGaps(ctx, repo, detectedBefore, batchSize)
		}
		pageProcessed, stop, err := r.processPendingPublishedAtCandidates(
			ctx,
			repo,
			tracking,
			candidates,
			runDeadline,
			resolveTimeout,
			failureBackoffTTL,
		)
		if err != nil {
			return err
		}
		processed += pageProcessed
		if stop {
			return nil
		}

		if nextCursor == nil {
			return r.recoverResolvedPublishedAtDispatchGaps(ctx, repo, detectedBefore, batchSize)
		}
		cursor = nextCursor
	}

	return r.recoverResolvedPublishedAtDispatchGaps(ctx, repo, detectedBefore, batchSize)
}

func (r *PendingPublishedAtResolver) recoverResolvedPublishedAtDispatchGaps(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	detectedBefore time.Time,
	limit int,
) error {
	if repo == nil {
		return fmt.Errorf("recover resolved published_at dispatch gaps: repository is nil")
	}

	gaps, err := repo.ListResolvedPublishedAtDispatchGaps(ctx, time.Now(), detectedBefore, limit)
	if err != nil {
		return fmt.Errorf("recover resolved published_at dispatch gaps: list candidates: %w", err)
	}
	if len(gaps) == 0 {
		return nil
	}

	tracking := trackingrepo.NewRepository(repo.db)
	retryAfter := func() time.Time {
		return time.Now().Add(r.resolverFailureBackoffTTL())
	}
	for i := range gaps {
		candidate := gaps[i].candidate
		observePublishedAtResolverScanned(candidate.Kind)
		finalizeResult, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, gaps[i].publishedAt, r.routeDecider)
		if err != nil {
			observePublishedAtResolverSkipped(candidate.Kind, "dispatch_gap_recovery_failed")
			r.markPublishedAtRetryAfterWithReporting(tracking, ctx, candidate, retryAfter(), false, "dispatch_gap_recovery_failed")
			r.logger.Warn("Resolved published_at dispatch gap recovery failed",
				slog.String("kind", string(candidate.Kind)),
				slog.String("post_id", candidate.PostID),
				slog.String("content_id", candidate.ContentID),
				slog.String("published_at", yttimestamp.Format(gaps[i].publishedAt)),
				slog.Any("error", err),
			)
			continue
		}
		if finalizeResult.enqueued && finalizeResult.reason == "resolved" {
			finalizeResult.reason = "resolved_dispatch_gap"
		}
		r.reportPendingPublishedAtCandidateResult(candidate, &gaps[i].publishedAt, finalizeResult)
		if !finalizeResult.enqueued && finalizeResult.reason != "already_claimed" && finalizeResult.reason != "already_sent" {
			r.markPublishedAtRetryAfterWithReporting(tracking, ctx, candidate, retryAfter(), false, "dispatch_gap_"+finalizeResult.reason)
		}
	}

	return nil
}

func (r *PendingPublishedAtResolver) processPendingPublishedAtCandidates(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	candidates []trackingrepo.PublishedAtResolutionCandidate,
	runDeadline time.Time,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
) (int, bool, error) {
	processed := 0
	for i := range candidates {
		result, err := r.processPendingPublishedAtCandidate(
			ctx,
			repo,
			tracking,
			candidates[i],
			runDeadline,
			resolveTimeout,
			failureBackoffTTL,
		)
		if err != nil {
			return processed, false, err
		}
		processed += result.processed
		if result.stop {
			return processed, true, nil
		}
	}

	return processed, false, nil
}

func (r *PendingPublishedAtResolver) processPendingPublishedAtCandidate(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	runDeadline time.Time,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
) (publishedAtResolverCandidateResult, error) {
	select {
	case <-ctx.Done():
		return publishedAtResolverCandidateResult{}, fmt.Errorf("run pending published_at resolver: parent context canceled: %w", ctx.Err())
	default:
	}

	observePublishedAtResolverScanned(candidate.Kind)
	if time.Now().After(runDeadline) {
		return publishedAtResolverCandidateResult{stop: true}, nil
	}
	if time.Until(runDeadline) < resolveTimeout {
		observePublishedAtResolverSkipped(candidate.Kind, "run_budget_exhausted")
		return publishedAtResolverCandidateResult{stop: true}, nil
	}

	result := publishedAtResolverCandidateResult{processed: 1}
	observePublishedAtResolutionAttempt(candidate.Kind)
	publishedAt, err := r.resolveCandidateWithTimeout(ctx, candidate, resolveTimeout)
	if err != nil {
		if errors.Is(err, errResolverParentCanceled) {
			return publishedAtResolverCandidateResult{}, fmt.Errorf("run pending published_at resolver: parent context canceled: %w", ctx.Err())
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
		return result, nil
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
		return result, nil
	}
	observePublishedAtResolutionSuccess(candidate.Kind)

	finalizeResult, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
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
		return result, nil
	}

	r.reportPendingPublishedAtCandidateResult(candidate, publishedAt, finalizeResult)
	return result, nil
}

func (r *PendingPublishedAtResolver) reportPendingPublishedAtCandidateResult(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt *time.Time,
	result publishedAtFinalizeResult,
) {
	if result.enqueued {
		observePublishedAtResolverEnqueued(candidate.Kind)
		r.logger.Info("published_at_resolver_enqueued",
			slog.String("kind", string(candidate.Kind)),
			slog.String("post_id", candidate.PostID),
			slog.String("channel_id", candidate.ChannelID),
			slog.String("published_at", yttimestamp.Format(*publishedAt)),
			slog.String("reason", result.reason),
		)
		return
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
