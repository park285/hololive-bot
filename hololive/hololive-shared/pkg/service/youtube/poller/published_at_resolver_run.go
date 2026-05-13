package poller

import (
	"context"
	"fmt"
	"log/slog"
	"time"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

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
	run := publishedAtResolverRun{
		maxResolvePerRun:  r.resolverMaxResolvePerRun(),
		runDeadline:       time.Now().Add(r.resolverMaxRunDuration()),
		resolveTimeout:    r.resolverResolveTimeout(),
		failureBackoffTTL: r.resolverFailureBackoffTTL(),
	}

	return r.processPendingPublishedAtResolutionPages(ctx, repo, tracking, detectedBefore, batchSize, run)
}

type publishedAtResolverRun struct {
	maxResolvePerRun  int
	runDeadline       time.Time
	resolveTimeout    time.Duration
	failureBackoffTTL time.Duration
}

type publishedAtResolverPageResult struct {
	processed   int
	cursor      *trackingrepo.PublishedAtResolutionCursor
	recoverGaps bool
	stop        bool
}

type publishedAtResolverPageStep struct {
	processed int
	cursor    *trackingrepo.PublishedAtResolutionCursor
	done      bool
}

func (r *PendingPublishedAtResolver) processPendingPublishedAtResolutionPages(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	detectedBefore time.Time,
	batchSize int,
	run publishedAtResolverRun,
) error {
	processed := 0
	var cursor *trackingrepo.PublishedAtResolutionCursor
	for processed < run.maxResolvePerRun {
		step, err := r.processPendingPublishedAtResolutionPageStep(ctx, repo, tracking, detectedBefore, batchSize, run, processed, cursor)
		if err != nil {
			return err
		}
		if step.done {
			return nil
		}
		processed += step.processed
		cursor = step.cursor
	}

	return r.recoverResolvedPublishedAtDispatchGaps(ctx, repo, detectedBefore, batchSize)
}

func (r *PendingPublishedAtResolver) processPendingPublishedAtResolutionPageStep(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	detectedBefore time.Time,
	batchSize int,
	run publishedAtResolverRun,
	processed int,
	cursor *trackingrepo.PublishedAtResolutionCursor,
) (publishedAtResolverPageStep, error) {
	if time.Now().After(run.runDeadline) {
		return publishedAtResolverPageStep{done: true}, nil
	}
	page, err := r.processPendingPublishedAtResolutionPage(ctx, repo, tracking, detectedBefore, batchSize, run, processed, cursor)
	if err != nil {
		return publishedAtResolverPageStep{}, err
	}
	done, err := r.finishPendingPublishedAtResolutionPage(ctx, repo, detectedBefore, batchSize, page)
	if err != nil {
		return publishedAtResolverPageStep{}, err
	}
	if done {
		return publishedAtResolverPageStep{done: true}, nil
	}

	return publishedAtResolverPageStep{
		processed: page.processed,
		cursor:    page.cursor,
	}, nil
}

func (r *PendingPublishedAtResolver) finishPendingPublishedAtResolutionPage(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	detectedBefore time.Time,
	batchSize int,
	page publishedAtResolverPageResult,
) (bool, error) {
	if page.stop {
		return true, nil
	}
	if page.recoverGaps {
		return true, r.recoverResolvedPublishedAtDispatchGaps(ctx, repo, detectedBefore, batchSize)
	}

	return false, nil
}

func (r *PendingPublishedAtResolver) processPendingPublishedAtResolutionPage(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	detectedBefore time.Time,
	batchSize int,
	run publishedAtResolverRun,
	processed int,
	cursor *trackingrepo.PublishedAtResolutionCursor,
) (publishedAtResolverPageResult, error) {
	candidates, nextCursor, err := tracking.ListPendingPublishedAtResolutionsPage(
		ctx,
		time.Now(),
		detectedBefore,
		cursor,
		minInt(batchSize, run.maxResolvePerRun-processed),
	)
	if err != nil {
		return publishedAtResolverPageResult{}, fmt.Errorf("run pending published_at resolver: list candidates: %w", err)
	}
	setPublishedAtResolverPageCandidates(len(candidates))
	if len(candidates) == 0 {
		return publishedAtResolverPageResult{recoverGaps: true}, nil
	}
	pageProcessed, stop, err := r.processPendingPublishedAtCandidates(
		ctx,
		repo,
		tracking,
		candidates,
		run.runDeadline,
		run.resolveTimeout,
		run.failureBackoffTTL,
	)
	if err != nil {
		return publishedAtResolverPageResult{}, err
	}

	return publishedAtResolverPageResult{
		processed:   pageProcessed,
		cursor:      nextCursor,
		recoverGaps: nextCursor == nil,
		stop:        stop,
	}, nil
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
		r.recoverResolvedPublishedAtDispatchGap(ctx, repo, tracking, gaps[i], retryAfter)
	}

	return nil
}

func (r *PendingPublishedAtResolver) recoverResolvedPublishedAtDispatchGap(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	gap resolvedPublishedAtDispatchGap,
	retryAfter func() time.Time,
) {
	candidate := gap.candidate
	observePublishedAtResolverScanned(candidate.Kind)
	finalizeResult, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, gap.publishedAt, r.routeDecider)
	if err != nil {
		r.reportResolvedPublishedAtDispatchGapRecoveryFailure(tracking, ctx, candidate, gap.publishedAt, retryAfter(), err)
		return
	}
	if finalizeResult.enqueued && finalizeResult.reason == "resolved" {
		finalizeResult.reason = "resolved_dispatch_gap"
	}
	r.reportPendingPublishedAtCandidateResult(candidate, &gap.publishedAt, finalizeResult)
	if !finalizeResult.enqueued && finalizeResult.reason != "already_claimed" && finalizeResult.reason != "already_sent" {
		r.markPublishedAtRetryAfterWithReporting(tracking, ctx, candidate, retryAfter(), false, "dispatch_gap_"+finalizeResult.reason)
	}
}

func (r *PendingPublishedAtResolver) reportResolvedPublishedAtDispatchGapRecoveryFailure(
	tracking *trackingrepo.GormRepository,
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	publishedAt time.Time,
	retryAfter time.Time,
	err error,
) {
	observePublishedAtResolverSkipped(candidate.Kind, "dispatch_gap_recovery_failed")
	r.markPublishedAtRetryAfterWithReporting(tracking, ctx, candidate, retryAfter, false, "dispatch_gap_recovery_failed")
	r.logger.Warn("Resolved published_at dispatch gap recovery failed",
		slog.String("kind", string(candidate.Kind)),
		slog.String("post_id", candidate.PostID),
		slog.String("content_id", candidate.ContentID),
		slog.String("published_at", yttimestamp.Format(publishedAt)),
		slog.Any("error", err),
	)
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
