package holodexprovider

import (
	"context"
	"log/slog"
	"sync"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/fallback"
)

type streamFetchState struct {
	mu         sync.Mutex
	allStreams []*domain.Stream
	seen       map[string]bool
}

func (h *Service) getStreamsByOrgWithFallback(ctx context.Context, plan *streamFetchPlan) ([]*domain.Stream, error) {
	if cached, found := getCachedStreamsByOrg(ctx, plan); found {
		return cached, nil
	}

	state := newStreamFetchState()
	targetOrgs := streamTargetOrgs(plan.resolvedOrg)
	primary := h.runStreamPrimaryFetches(ctx, plan, targetOrgs, state)
	fallback.ObservePrimaryPhase("holodex", plan.operation, len(targetOrgs), primary.Succeeded, len(primary.Failed))

	h.scheduleStreamRetryIfNeeded(ctx, plan, primary)

	secondary, err := h.runStreamScraperFallback(ctx, plan, primary, state)
	if err == nil && secondary.Outcome == "hit" {
		return state.streams(), nil
	}

	cacheStreamsByOrg(ctx, plan, state.streams())

	return state.streams(), nil
}

func newStreamFetchState() *streamFetchState {
	return &streamFetchState{seen: make(map[string]bool)}
}

func getCachedStreamsByOrg(ctx context.Context, plan *streamFetchPlan) ([]*domain.Stream, bool) {
	if plan.cacheGet == nil {
		return nil, false
	}
	return plan.cacheGet(ctx, plan.resolvedOrg, plan.hours)
}

func (h *Service) runStreamPrimaryFetches(ctx context.Context, plan *streamFetchPlan, targetOrgs []string, state *streamFetchState) fallback.PrimaryResult[string] {
	return fallback.RunPrimary(ctx, targetOrgs, fallback.FetchPlan[string, struct{}]{
		Parallelism: holodexOrgFetchParallelism(plan.resolvedOrg, h.concurrency.OrgAllParallelism),
	}, func(fetchCtx context.Context, targetOrg string) error {
		return h.fetchAndStoreStreamsForOrg(fetchCtx, targetOrg, plan, state)
	})
}

func (h *Service) fetchAndStoreStreamsForOrg(ctx context.Context, targetOrg string, plan *streamFetchPlan, state *streamFetchState) error {
	streams, err := h.fetchStreamsByOrg(ctx, targetOrg, plan.status, plan.hours)
	if err != nil {
		h.logger.Warn("Failed to get streams for org",
			slog.String("org", targetOrg),
			slog.String("status", plan.status),
			slog.Any("error", err),
		)
		return err
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
		if !state.seen[stream.ID] {
			state.seen[stream.ID] = true
			state.allStreams = append(state.allStreams, stream)
		}
	}
}

func (state *streamFetchState) replaceStreams(streams []*domain.Stream) {
	state.mu.Lock()
	defer state.mu.Unlock()
	state.allStreams = streams
}

func (state *streamFetchState) streams() []*domain.Stream {
	state.mu.Lock()
	defer state.mu.Unlock()
	return state.allStreams
}

func (h *Service) scheduleStreamRetryIfNeeded(ctx context.Context, plan *streamFetchPlan, primary fallback.PrimaryResult[string]) {
	if !primary.HasFailures() || plan.retry == nil {
		return
	}
	h.scheduleRetryIfNeeded(ctx, plan.retryKey, func(retryCtx context.Context) {
		plan.retry(retryCtx, plan.resolvedOrg, plan.hours)
	})
}

func (h *Service) runStreamScraperFallback(ctx context.Context, plan *streamFetchPlan, primary fallback.PrimaryResult[string], state *streamFetchState) (fallback.SecondaryExecution, error) {
	scraperFallbackPolicy := fallback.Policy{Trigger: fallback.TriggerOnEmptyPrimaryWithError}
	return fallback.RunSecondary(ctx, fallback.SecondaryPlan{
		Service:   "holodex",
		Operation: plan.operation,
		Trigger:   scraperFallbackPolicy.Trigger,
		ShouldRun: h.shouldRunStreamScraperFallback(plan, primary, scraperFallbackPolicy, state),
		Run: func(runCtx context.Context) (fallback.SecondaryResult, error) {
			return h.runStreamScraperFallbackFetch(runCtx, plan, primary, state)
		},
	})
}

func (h *Service) shouldRunStreamScraperFallback(plan *streamFetchPlan, primary fallback.PrimaryResult[string], policy fallback.Policy, state *streamFetchState) bool {
	return h.scraper != nil && supportsScraperFallback(plan.resolvedOrg) &&
		policy.ShouldRun(len(state.streams()), len(primary.Failed))
}

func (h *Service) runStreamScraperFallbackFetch(ctx context.Context, plan *streamFetchPlan, primary fallback.PrimaryResult[string], state *streamFetchState) (fallback.SecondaryResult, error) {
	h.logger.Warn(plan.fallbackLogMessage,
		slog.Int("failed_orgs", len(primary.Failed)),
	)
	scraperStreams, err := h.scraper.FetchAllStreams(ctx)
	if err != nil {
		return fallback.SecondaryResult{}, err
	}
	if plan.scraperFilter != nil {
		scraperStreams = plan.scraperFilter(scraperStreams)
	}
	scraperStreams = limitStreamList(scraperStreams)
	cacheStreamsByOrg(ctx, plan, scraperStreams)
	state.replaceStreams(scraperStreams)
	return fallback.SecondaryResult{
		Items:     len(scraperStreams),
		Successes: 1,
	}, nil
}

func cacheStreamsByOrg(ctx context.Context, plan *streamFetchPlan, streams []*domain.Stream) {
	if plan.cacheSet != nil {
		plan.cacheSet(ctx, plan.resolvedOrg, plan.hours, streams)
	}
}
