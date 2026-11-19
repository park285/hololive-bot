package holodexprovider

import (
	"context"
	"errors"
	"fmt"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/constants"
	"github.com/kapu/hololive-shared/pkg/domain"
)

type streamFetchState struct {
	mu          sync.Mutex
	allStreams  []*domain.Stream
	seen        map[string]bool
	fetchErrors []error
}

func (h *Service) getStreamsByOrgWithFallback(ctx context.Context, plan *streamFetchPlan) ([]*domain.Stream, error) {
	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("get streams: %w", err)
	}

	if cached, found := getCachedStreamsByOrg(ctx, plan); found {
		return cached, nil
	}

	cacheKey := plan.cacheKey()
	if err := h.streamCacheFills.acquire(ctx, cacheKey); err != nil {
		return nil, fmt.Errorf("acquire stream cache fill: %w", err)
	}

	defer h.streamCacheFills.release(cacheKey)

	if cached, found := getCachedStreamsByOrg(ctx, plan); found {
		return cached, nil
	}

	if err := ctx.Err(); err != nil {
		return nil, fmt.Errorf("fetch streams: %w", err)
	}

	state := newStreamFetchState()
	targetOrgs := streamTargetOrgs(plan.resolvedOrg)
	primary := h.runStreamPrimaryFetches(ctx, plan, targetOrgs, state)
	observeStreamPrimary(plan.operation, primary)

	if primary.wasCanceled() {
		return nil, fmt.Errorf("fetch streams: %w", context.Cause(ctx))
	}

	h.scheduleStreamRetryIfNeeded(ctx, plan, primary)

	if !primary.hasFailures() {
		streams := state.streams()
		cacheStreamsByOrg(ctx, plan, streams)

		return streams, nil
	}

	if primary.Succeeded > 0 {
		return state.streams(), &PartialStreamsError{
			Operation:  plan.operation,
			FailedOrgs: primary.Failed,
			Err:        state.primaryError(plan),
		}
	}

	out, err := h.resolveFailedPrimary(ctx, plan, primary, state)
	if err != nil {
		return out, fmt.Errorf("resolve failed primary: %w", err)
	}

	return out, nil
}

type PartialStreamsError struct {
	Operation  string
	FailedOrgs []string
	Err        error
}

func (e *PartialStreamsError) Error() string {
	return fmt.Sprintf("holodex %s partial result: failed orgs %v: %v", e.Operation, e.FailedOrgs, e.Err)
}

func (e *PartialStreamsError) Unwrap() error {
	return e.Err
}

func (h *Service) resolveFailedPrimary(
	ctx context.Context,
	plan *streamFetchPlan,
	primary orgFetchResult,
	state *streamFetchState,
) ([]*domain.Stream, error) {
	outcome, err := h.runOfficialScheduleFallback(ctx, plan, primary, state)
	if err != nil {
		return nil, errors.Join(state.primaryError(plan), fmt.Errorf("official schedule fallback: %w", err))
	}

	if outcome == streamFallbackOutcomeHit {
		streams := state.streams()
		cacheStreamsByOrg(ctx, plan, streams)

		return streams, nil
	}

	return nil, state.primaryError(plan)
}

func newStreamFetchState() *streamFetchState {
	return &streamFetchState{seen: make(map[string]bool)}
}

func getCachedStreamsByOrg(ctx context.Context, plan *streamFetchPlan) ([]*domain.Stream, bool) {
	if plan == nil || plan.cacheGet == nil {
		return nil, false
	}

	return plan.cacheGet(ctx, plan.resolvedOrg, plan.hours)
}

func (h *Service) runStreamPrimaryFetches(
	ctx context.Context,
	plan *streamFetchPlan,
	targetOrgs []string,
	state *streamFetchState,
) orgFetchResult {
	parallelism := holodexOrgFetchParallelism(plan.resolvedOrg, h.concurrency.OrgAllParallelism)

	return runOrgFetches(ctx, parallelism, targetOrgs, func(fetchCtx context.Context, targetOrg string) error {
		return h.fetchAndStoreStreamsForOrg(fetchCtx, targetOrg, plan, state)
	})
}

func (h *Service) fetchAndStoreStreamsForOrg(
	ctx context.Context,
	targetOrg string,
	plan *streamFetchPlan,
	state *streamFetchState,
) error {
	streams, err := h.fetchStreamsByOrg(ctx, targetOrg, plan.status, plan.hours)
	if err != nil {
		state.addError(err)
		h.logger.Warn("Failed to get streams for org",
			slog.String("org", targetOrg),
			slog.String("status", plan.status),
			slog.Any("error", err))

		return fmt.Errorf("fetch streams by org: %w", err)
	}

	filtered := h.filter.FilterHololiveStreams(streams)

	filtered = filterStreamsByRequestedOrg(filtered, plan.resolvedOrg)

	if plan.primaryFilter != nil {
		filtered = plan.primaryFilter(filtered)
	}

	state.addStreams(filtered)

	return nil
}

func (state *streamFetchState) addStreams(streams []*domain.Stream) {
	state.mu.Lock()
	defer state.mu.Unlock()

	for _, stream := range streams {
		if stream == nil || state.seen[stream.ID] {
			continue
		}

		state.seen[stream.ID] = true
		state.allStreams = append(state.allStreams, stream)
	}
}

func (state *streamFetchState) addError(err error) {
	if err == nil {
		return
	}

	state.mu.Lock()

	state.fetchErrors = append(state.fetchErrors, err)
	state.mu.Unlock()
}

func (state *streamFetchState) replaceStreams(streams []*domain.Stream) {
	state.mu.Lock()

	state.allStreams = append(state.allStreams[:0], streams...)
	state.seen = make(map[string]bool, len(streams))

	for _, stream := range streams {
		if stream != nil {
			state.seen[stream.ID] = true
		}
	}

	state.mu.Unlock()
}

func (state *streamFetchState) streams() []*domain.Stream {
	state.mu.Lock()
	defer state.mu.Unlock()

	return append([]*domain.Stream(nil), state.allStreams...)
}

func (state *streamFetchState) primaryError(plan *streamFetchPlan) error {
	state.mu.Lock()

	joined := errors.Join(state.fetchErrors...)
	state.mu.Unlock()

	if joined != nil {
		return fmt.Errorf("holodex %s primary failed: %w", plan.operation, joined)
	}

	return fmt.Errorf("holodex %s primary failed", plan.operation)
}

func (h *Service) scheduleStreamRetryIfNeeded(
	ctx context.Context,
	plan *streamFetchPlan,
	primary orgFetchResult,
) {
	if !primary.hasFailures() || plan.retry == nil {
		return
	}

	h.scheduleRetryIfNeeded(ctx, plan.retryKey, func(retryCtx context.Context) {
		plan.retry(retryCtx, plan.resolvedOrg, plan.hours)
	})
}

func (h *Service) runOfficialScheduleFallback(
	ctx context.Context,
	plan *streamFetchPlan,
	primary orgFetchResult,
	state *streamFetchState,
) (string, error) {
	if !h.shouldRunOfficialScheduleFallback(plan, primary, state) {
		observeStreamFallbackExecution(plan.operation, streamFallbackOutcomeSkipped)

		return streamFallbackOutcomeSkipped, nil
	}

	items, err := h.runOfficialScheduleFallbackFetch(ctx, plan, primary, state)
	if err != nil {
		observeStreamFallbackExecution(plan.operation, streamFallbackOutcomeError)

		return streamFallbackOutcomeError, err
	}

	outcome := streamFallbackOutcomeMiss

	if items > 0 {
		outcome = streamFallbackOutcomeHit
	}

	observeStreamFallbackExecution(plan.operation, outcome)

	return outcome, nil
}

func (h *Service) shouldRunOfficialScheduleFallback(
	plan *streamFetchPlan,
	primary orgFetchResult,
	state *streamFetchState,
) bool {
	return h.scraper != nil && supportsOfficialScheduleFallback(plan) &&
		len(state.streams()) == 0 && primary.hasFailures()
}

func supportsOfficialScheduleFallback(plan *streamFetchPlan) bool {
	return plan != nil &&
		plan.operation == "upcoming_streams" &&
		plan.resolvedOrg == constants.HolodexAPIParams.OrgHololive
}

func (h *Service) runOfficialScheduleFallbackFetch(
	ctx context.Context,
	plan *streamFetchPlan,
	primary orgFetchResult,
	state *streamFetchState,
) (int, error) {
	h.logger.Warn(plan.fallbackLogMessage, slog.Int("failed_orgs", len(primary.Failed)))

	streams, err := h.scraper.FetchUpcomingStreams(ctx, plan.hours)
	if err != nil {
		return 0, fmt.Errorf("fetch upcoming streams: %w", err)
	}

	if plan.fallbackFilter != nil {
		streams = plan.fallbackFilter(streams)
	}

	streams = limitStreamList(streams)
	state.replaceStreams(streams)

	return len(streams), nil
}

func cacheStreamsByOrg(ctx context.Context, plan *streamFetchPlan, streams []*domain.Stream) {
	if plan != nil && plan.cacheSet != nil {
		plan.cacheSet(ctx, plan.resolvedOrg, plan.hours, streams)
	}
}
