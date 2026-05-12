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
