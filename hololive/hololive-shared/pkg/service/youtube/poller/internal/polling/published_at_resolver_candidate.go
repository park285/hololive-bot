package polling

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"strings"
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

	claim, claimed, err := r.tryClaimPendingPublishedAtCandidate(ctx, candidate, resolveTimeout, failureBackoffTTL)
	if err != nil {
		return publishedAtResolverCandidateResult{}, err
	}
	if !claimed {
		return publishedAtResolverCandidateResult{}, nil
	}
	releaseClaim := true
	defer func() {
		if releaseClaim && claim != nil {
			r.releasePendingPublishedAtCandidateClaim(ctx, candidate, claim)
		}
	}()

	result, completed, err := r.processClaimedPendingPublishedAtCandidate(
		ctx,
		repo,
		tracking,
		candidate,
		resolveTimeout,
		failureBackoffTTL,
		claim,
	)
	if completed {
		releaseClaim = false
	}
	return result, err
}

func (r *PendingPublishedAtResolver) processClaimedPendingPublishedAtCandidate(
	ctx context.Context,
	repo *publishedAtResolverRepository,
	tracking *trackingrepo.GormRepository,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
	claim JobClaim,
) (publishedAtResolverCandidateResult, bool, error) {
	result := publishedAtResolverCandidateResult{processed: 1}
	observePublishedAtResolutionAttempt(candidate.Kind)
	publishedAt, err := r.resolveCandidateWithTimeout(ctx, candidate, resolveTimeout)
	if err != nil {
		result, err := r.handlePendingPublishedAtResolveError(
			ctx,
			tracking,
			candidate,
			result,
			err,
			resolveTimeout,
			failureBackoffTTL,
		)
		return result, false, err
	}
	if publishedAt == nil || publishedAt.IsZero() {
		r.handleEmptyPendingPublishedAt(ctx, tracking, candidate, failureBackoffTTL)
		return result, false, nil
	}
	observePublishedAtResolutionSuccess(candidate.Kind)

	finalizeResult, err := repo.FinalizePublishedAtAndMaybeEnqueue(ctx, candidate, *publishedAt, r.routeDecider)
	if err != nil {
		r.handlePendingPublishedAtFinalizeError(ctx, tracking, candidate, err, failureBackoffTTL)
		return result, false, nil
	}

	r.reportPendingPublishedAtCandidateResult(candidate, publishedAt, finalizeResult)
	if err := r.completePendingPublishedAtCandidateClaim(ctx, candidate, claim, failureBackoffTTL); err != nil {
		return result, false, err
	}
	return result, true, nil
}

func (r *PendingPublishedAtResolver) tryClaimPendingPublishedAtCandidate(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	resolveTimeout time.Duration,
	failureBackoffTTL time.Duration,
) (JobClaim, bool, error) {
	if r == nil || r.candidateClaimer == nil {
		return nil, true, nil
	}

	leaseTTL := max(resolveTimeout+5*time.Second, time.Minute)
	status, claim, err := r.candidateClaimer.TryClaim(
		ctx,
		PendingPublishedAtResolverCandidatePollerName,
		publishedAtCandidateClaimID(candidate),
		leaseTTL,
		failureBackoffTTL,
	)
	if err != nil {
		observeJobClaim(PendingPublishedAtResolverCandidatePollerName, string(JobClaimUnavailable))
		return nil, false, fmt.Errorf("claim pending published_at candidate: %w", err)
	}
	observeJobClaim(PendingPublishedAtResolverCandidatePollerName, string(status.Result))
	return r.resolvePendingPublishedAtCandidateClaimStatus(candidate, status, claim)
}

func (r *PendingPublishedAtResolver) resolvePendingPublishedAtCandidateClaimStatus(
	candidate trackingrepo.PublishedAtResolutionCandidate,
	status JobClaimStatus,
	claim JobClaim,
) (JobClaim, bool, error) {
	switch status.Result {
	case JobClaimAcquired:
		return requirePendingPublishedAtCandidateClaimHandle(claim)
	case JobClaimPeerOwned, JobClaimAlreadyCompleted:
		observePublishedAtResolverSkipped(candidate.Kind, string(status.Result))
		return nil, false, nil
	case JobClaimUnavailable:
		return nil, false, fmt.Errorf("claim pending published_at candidate: unavailable")
	default:
		return nil, false, fmt.Errorf("claim pending published_at candidate: unexpected result %q", status.Result)
	}
}

func requirePendingPublishedAtCandidateClaimHandle(claim JobClaim) (JobClaim, bool, error) {
	if claim == nil {
		return nil, false, fmt.Errorf("claim pending published_at candidate: acquired without claim")
	}
	return claim, true, nil
}

func publishedAtCandidateClaimID(candidate trackingrepo.PublishedAtResolutionCandidate) string {
	contentID := strings.TrimSpace(candidate.ContentID)
	if contentID == "" {
		contentID = strings.TrimSpace(candidate.PostID)
	}
	if contentID == "" {
		return string(candidate.Kind)
	}
	return string(candidate.Kind) + ":" + contentID
}

func (r *PendingPublishedAtResolver) completePendingPublishedAtCandidateClaim(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	claim JobClaim,
	cooldownTTL time.Duration,
) error {
	if claim == nil {
		return nil
	}
	completed, err := claim.MarkCompleted(ctx, cooldownTTL)
	observeJobMarkCompleted(PendingPublishedAtResolverCandidatePollerName, boolResult(completed, err))
	if err != nil {
		return fmt.Errorf("complete pending published_at candidate claim: %w", err)
	}
	if !completed {
		return fmt.Errorf("complete pending published_at candidate claim: ownership lost")
	}
	r.logger.Debug("Pending published_at candidate claim completed",
		slog.String("kind", string(candidate.Kind)),
		slog.String("post_id", candidate.PostID),
		slog.String("content_id", candidate.ContentID),
	)
	return nil
}

func (r *PendingPublishedAtResolver) releasePendingPublishedAtCandidateClaim(
	ctx context.Context,
	candidate trackingrepo.PublishedAtResolutionCandidate,
	claim JobClaim,
) {
	released, err := claim.Release(ctx)
	observeJobRelease(PendingPublishedAtResolverCandidatePollerName, boolResult(released, err))
	if err != nil {
		r.logger.Warn("Pending published_at candidate claim release failed",
			slog.String("kind", string(candidate.Kind)),
			slog.String("post_id", candidate.PostID),
			slog.String("content_id", candidate.ContentID),
			slog.Any("error", err),
		)
		return
	}
	if !released {
		r.logger.Warn("Pending published_at candidate claim release skipped after ownership loss",
			slog.String("kind", string(candidate.Kind)),
			slog.String("post_id", candidate.PostID),
			slog.String("content_id", candidate.ContentID),
		)
	}
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
