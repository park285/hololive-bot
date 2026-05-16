package polling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"time"

	yttimestamp "github.com/kapu/hololive-shared/pkg/service/youtube/timestamp"
	trackingrepo "github.com/kapu/hololive-shared/pkg/service/youtube/tracking"
)

func (r *PendingPublishedAtResolver) processPendingPublishedAtCandidate(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	runDeadline time.Time,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
) (publishedAtResolverCandidateResult, error) {
	budgetResult, stop, err := checkPendingPublishedAtCandidateBudget(ctx, candidate, runDeadline, resolveTimeout)
	if stop || err != nil {
		return budgetResult, err
	}

	result := publishedAtResolverCandidateResult{processed: 1}
	observePublishedAtResolutionAttempt(candidate.Kind)
	publishedAt, err := r.resolveCandidateWithTimeout(ctx, candidate, resolveTimeout)
	if err != nil {
		return r.handlePendingPublishedAtResolveError(
			ctx,
			tracking,
			candidate,
			result,
			err,
			resolveTimeout,
			failureBackoffTTL,
		)
	}
	if publishedAt == nil || publishedAt.IsZero() {
		r.handleEmptyPendingPublishedAt(ctx, tracking, candidate, failureBackoffTTL)
		return result, nil
	}
	observePublishedAtResolutionSuccess(candidate.Kind)

	finalizeResult, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
	if err != nil {
		r.handlePendingPublishedAtFinalizeError(ctx, tracking, candidate, err, failureBackoffTTL)
		return result, nil
	}

	r.reportPendingPublishedAtCandidateResult(candidate, publishedAt, finalizeResult)
	return result, nil
}

func checkPendingPublishedAtCandidateBudget(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	runDeadline time.Time,
	resolveTimeout time.Duration,
) (publishedAtResolverCandidateResult, bool, error) {
	select {
	case <-ctx.Done():
		return publishedAtResolverCandidateResult{}, false, fmt.Errorf("run pending published_at resolver: parent context canceled: %w", ctx.Err())
	default:
	}

	observePublishedAtResolverScanned(candidate.Kind)
	if time.Now().After(runDeadline) {
		return publishedAtResolverCandidateResult{stop: true}, true, nil
	}
	if time.Until(runDeadline) < resolveTimeout {
		observePublishedAtResolverSkipped(candidate.Kind, "run_budget_exhausted")
		return publishedAtResolverCandidateResult{stop: true}, true, nil
	}

	return publishedAtResolverCandidateResult{}, false, nil
}

func (r *PendingPublishedAtResolver) handlePendingPublishedAtResolveError(
	ctx context.Context,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	result publishedAtResolverCandidateResult,
	err error,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
) (publishedAtResolverCandidateResult, error) {
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

func (r *PendingPublishedAtResolver) handleEmptyPendingPublishedAt(
	ctx context.Context,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	failureBackoffTTL time.Duration,
) {
	r.markPublishedAtRetryAfterWithReporting(
		tracking,
		ctx,
		candidate,
		time.Now().Add(failureBackoffTTL),
		false,
		"published_at_empty",
	)
	observePublishedAtResolverSkipped(candidate.Kind, "published_at_empty")
}

func (r *PendingPublishedAtResolver) handlePendingPublishedAtFinalizeError(
	ctx context.Context,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	err error,
	failureBackoffTTL time.Duration,
) {
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
