package collectorruntime

import (
	"context"
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log/slog"
	"strings"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/config/settings"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
	"github.com/kapu/hololive-shared/pkg/service/youtube/sourceobservation"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collecterr"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/collectutil"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/holodexcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/joblease"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/officialcollector"
	"github.com/kapu/hololive-youtube-collector/internal/runtime/youtubejscollector"
)

type leaseScheduler struct {
	repository *joblease.Repository
	registry   *Registry
	publisher  *Publisher
	metrics    *Metrics
	owner      string
	logger     *slog.Logger
	config     joblease.Config
	collector  settings.YouTubeCollectorConfig
	gates      map[contract.Provider]chan struct{}

	mu     sync.Mutex
	queued map[string]struct{}
	queue  chan joblease.JobSpec
	cancel context.CancelFunc
	wg     sync.WaitGroup
}

func leaseConfigFrom(cfg settings.YouTubeCollectorConfig, holodexTimeout, officialTimeout time.Duration) (joblease.Config, error) {
	lease := joblease.Config{
		LeaseTTL:            cfg.LeaseTTL,
		RenewInterval:       cfg.RenewInterval,
		ProviderTimeout:     cfg.MaxProviderTimeout(holodexTimeout, officialTimeout),
		NormalizationBudget: cfg.NormalizationBudget,
		PublishBudget:       cfg.PublishBudget,
		MinRetryDelay:       cfg.RetryMin,
		MaxRetryDelay:       cfg.RetryMax,
		MinReleaseJitter:    cfg.ReleaseJitterMin,
		MaxReleaseJitter:    cfg.ReleaseJitterMax,
		AcquisitionBatch:    cfg.AcquisitionBatch,
		WorkerCount:         cfg.TotalWorkers,
		QueueCapacity:       cfg.QueueCapacity,
		PollCadence:         cfg.AcquisitionCadence,
	}
	if err := lease.Validate(); err != nil {
		return joblease.Config{}, err
	}
	return lease, nil
}

func buildScheduler(
	appConfig *settings.Config,
	infra *collectorInfrastructure,
	logger *slog.Logger,
) (*leaseScheduler, error) {
	if appConfig == nil || infra == nil || infra.postgres == nil || infra.postgres.GetPool() == nil {
		return nil, fmt.Errorf("build youtube collector: postgres pool is required")
	}
	if infra.youtubejs == nil || infra.youtubejsRPC == nil || infra.holodex == nil || infra.official == nil {
		return nil, fmt.Errorf("build youtube collector: provider clients are required")
	}
	collector := appConfig.YouTubeCollector.OrDefault()
	if err := collector.Validate(appConfig.Holodex.Timeout, appConfig.OfficialSchedule.Timeout); err != nil {
		return nil, fmt.Errorf("build youtube collector: %w", err)
	}
	leaseConfig, err := leaseConfigFrom(collector, appConfig.Holodex.Timeout, appConfig.OfficialSchedule.Timeout)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: lease config: %w", err)
	}
	repository, err := joblease.NewRepository(infra.postgres.GetPool(), leaseConfig)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: collection lease repository: %w", err)
	}
	maxResults := collectutil.DefaultMaxResults()
	registry, err := NewRegistry(
		youtubejscollector.NewCommunityRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewContentRunner(infra.youtubejsRPC, maxResults),
		youtubejscollector.NewChannelRunner(infra.youtubejsRPC),
		youtubejscollector.NewViewerRunner(infra.youtubejsRPC),
		holodexcollector.NewRunner(infra.holodex),
		officialcollector.NewRunner(infra.official),
	)
	if err != nil {
		return nil, fmt.Errorf("build youtube collector: job registry: %w", err)
	}
	owner, err := newCollectorOwner(collector.InstanceID)
	if err != nil {
		return nil, err
	}
	return &leaseScheduler{
		repository: repository,
		registry:   registry,
		publisher:  NewPublisher(infra.postgres.GetPool()),
		metrics:    NewMetrics(nil),
		owner:      owner,
		logger:     logger,
		config:     leaseConfig,
		collector:  collector,
		gates:      newProviderGates(collector),
		queued:     make(map[string]struct{}),
		queue:      make(chan joblease.JobSpec, collector.QueueCapacity),
	}, nil
}

func newProviderGates(cfg settings.YouTubeCollectorConfig) map[contract.Provider]chan struct{} {
	return map[contract.Provider]chan struct{}{
		contract.ProviderHolodex:          make(chan struct{}, cfg.HolodexMaxInflight),
		contract.ProviderHololiveOfficial: make(chan struct{}, cfg.OfficialMaxInflight),
		contract.ProviderYouTubeJS:        make(chan struct{}, cfg.YouTubeJSMaxInflight),
	}
}

func newCollectorOwner(instanceID string) (string, error) {
	token := make([]byte, 16)
	if _, err := rand.Read(token); err != nil {
		return "", fmt.Errorf("build youtube collector: generate lease owner: %w", err)
	}
	prefix := runtimeName
	if id := strings.TrimSpace(instanceID); id != "" {
		prefix = id
	}
	return prefix + ":" + hex.EncodeToString(token), nil
}

func (s *leaseScheduler) Start(ctx context.Context) {
	if s == nil || s.repository == nil || s.registry == nil {
		return
	}
	s.mu.Lock()
	if s.cancel != nil {
		s.mu.Unlock()
		return
	}
	runCtx, cancel := context.WithCancel(ctx)
	s.cancel = cancel
	s.mu.Unlock()
	for i := 0; i < s.config.WorkerCount; i++ {
		s.wg.Add(1)
		go s.worker(runCtx)
	}
	s.wg.Add(1)
	go s.discover(runCtx)
}

func (s *leaseScheduler) Stop() {
	if s == nil {
		return
	}
	s.mu.Lock()
	cancel := s.cancel
	s.cancel = nil
	s.mu.Unlock()
	if cancel != nil {
		cancel()
		s.wg.Wait()
	}
}

func (s *leaseScheduler) discover(ctx context.Context) {
	defer s.wg.Done()
	ticker := time.NewTicker(s.config.PollCadence)
	defer ticker.Stop()
	s.syncCandidates(ctx)
	s.refreshFreshness(time.Now().UTC())
	for {
		select {
		case <-ctx.Done():
			return
		case <-ticker.C:
			s.syncCandidates(ctx)
			s.refreshFreshness(time.Now().UTC())
		}
	}
}

func (s *leaseScheduler) refreshFreshness(now time.Time) {
	if s.metrics == nil || s.registry == nil {
		return
	}
	for _, runner := range s.registry.Runners() {
		s.metrics.ObserveFreshness(runner.Provider(), runner.JobKind(), now)
	}
}

func (s *leaseScheduler) syncCandidates(ctx context.Context) {
	var globals []joblease.JobSpec
	var subjects []joblease.JobSpec
	for _, runner := range s.registry.Runners() {
		candidates, err := s.repository.Candidates(ctx, runner.Provider(), runner.JobKind(), s.config.AcquisitionBatch)
		if err != nil {
			if !errors.Is(err, joblease.ErrProjectionStale) && !errors.Is(err, joblease.ErrTargetDisabled) {
				s.logFailure("candidate_load", collecterr.CandidateFailed, joblease.JobSpec{
					Provider: runner.Provider(), CollectionJobKind: runner.JobKind(),
				}, contract.LeaseProof{})
			}
			continue
		}
		for _, candidate := range candidates {
			if candidate.Class == "GLOBAL" {
				globals = append(globals, candidate)
				continue
			}
			subjects = append(subjects, candidate)
		}
	}
	if !s.enqueueAll(ctx, globals) {
		return
	}
	s.enqueueAll(ctx, subjects)
}

func (s *leaseScheduler) enqueueAll(ctx context.Context, candidates []joblease.JobSpec) bool {
	for _, candidate := range candidates {
		if !s.enqueue(ctx, candidate) {
			return false
		}
	}
	return true
}

func (s *leaseScheduler) enqueue(ctx context.Context, candidate joblease.JobSpec) bool {
	if !s.markQueued(candidate.JobKey) {
		return true
	}
	select {
	case s.queue <- candidate:
		return true
	case <-ctx.Done():
		s.unmarkQueued(candidate.JobKey)
		return false
	default:
		s.unmarkQueued(candidate.JobKey)
		return true
	}
}

func (s *leaseScheduler) worker(ctx context.Context) {
	defer s.wg.Done()
	for {
		select {
		case <-ctx.Done():
			return
		case spec := <-s.queue:
			s.runSpec(ctx, spec)
			s.unmarkQueued(spec.JobKey)
		}
	}
}

func (s *leaseScheduler) runSpec(ctx context.Context, spec joblease.JobSpec) {
	runner, ok := s.registry.Lookup(spec.Provider, spec.CollectionJobKind)
	if !ok {
		s.logFailure("collect", collecterr.Failed, spec, contract.LeaseProof{})
		return
	}
	lease, err := s.repository.Acquire(ctx, spec, s.owner)
	if errors.Is(err, joblease.ErrNotAcquired) {
		s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultNotAcquired)
		return
	}
	if err != nil {
		if !errors.Is(err, joblease.ErrProjectionStale) && !errors.Is(err, joblease.ErrTargetDisabled) {
			s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultError)
			s.logFailure("acquire", collecterr.AcquireFailed, spec, contract.LeaseProof{})
		}
		return
	}
	s.metrics.ObserveAcquire(spec.Provider, spec.CollectionJobKind, resultAcquired)
	proof := lease.Proof()
	started := time.Now()
	err = s.repository.Run(ctx, lease, func(runCtx context.Context, leaseProof contract.LeaseProof) error {
		return s.collectAndPublish(runCtx, runner, spec, lease, leaseProof)
	})
	s.metrics.ObserveAttempt(spec.Provider, spec.CollectionJobKind, attemptResult(err), time.Since(started))
	if err == nil || errors.Is(err, context.Canceled) || errors.Is(err, joblease.ErrFenceLost) ||
		errors.Is(err, sourceobservation.ErrProjectionStale) || errors.Is(err, sourceobservation.ErrTargetDisabled) {
		if errors.Is(err, joblease.ErrFenceLost) {
			s.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phaseCollect)
		}
		return
	}
	retryAt := s.retryAt(err)
	if deferErr := lease.Defer(ctx, retryAt, collecterr.Code(err)); deferErr != nil && !errors.Is(deferErr, joblease.ErrFenceLost) {
		s.logFailure("defer", collecterr.DeferFailed, spec, proof)
		return
	}
	s.logFailure("collect", collecterr.Code(err), spec, proof)
}

func (s *leaseScheduler) collectAndPublish(
	ctx context.Context,
	runner JobRunner,
	spec joblease.JobSpec,
	lease joblease.Lease,
	proof contract.LeaseProof,
) error {
	generations, err := s.publisher.LoadContractGenerations(ctx, spec.Provider, runner.Emissions())
	if err != nil {
		return err
	}
	enabled, err := s.loadEnabledSubjects(ctx, proof.ProjectionGeneration, runner.Emissions())
	if err != nil {
		return err
	}
	if err := s.acquireProvider(ctx, spec.Provider); err != nil {
		return collecterr.FromContext(err)
	}
	defer s.releaseProvider(spec.Provider)
	output, err := runner.Collect(ctx, collectutil.RunInput{
		Spec:                spec,
		Lease:               proof,
		ContractGenerations: generations,
		MaxPages:            s.collector.MaxPages,
		MaxAggregateBytes:   s.collector.MaxAggregateBytes,
		RequestedChannelIDs: collectutil.UniqueSorted(enabled[contract.KindLiveSnapshot]),
		EnabledSubjects:     enabled,
	})
	if err != nil {
		return err
	}
	if len(output.Observations) == 0 {
		s.metrics.ObservePublish(spec.Provider, spec.CollectionJobKind, outcomeEmpty)
		return lease.Complete(ctx)
	}
	publishCtx, cancel := context.WithTimeout(ctx, s.config.PublishBudget)
	defer cancel()
	result, err := s.publisher.Publish(publishCtx, proof, output)
	if err != nil {
		if errors.Is(err, joblease.ErrFenceLost) {
			s.metrics.ObserveLeaseLost(spec.Provider, spec.CollectionJobKind, phasePublish)
		}
		s.observeRejected(spec.Provider, output)
		return err
	}
	s.observePublished(output, result)
	s.metrics.ObserveSuccess(spec.Provider, spec.CollectionJobKind, time.Now().UTC())
	return nil
}

func (s *leaseScheduler) loadEnabledSubjects(
	ctx context.Context,
	generation int64,
	kinds []contract.ObservationKind,
) (map[contract.ObservationKind][]string, error) {
	result := make(map[contract.ObservationKind][]string, len(kinds))
	for _, kind := range kinds {
		subjects, err := s.repository.EnabledSubjects(ctx, generation, kind)
		if err != nil {
			return nil, err
		}
		result[kind] = subjects
	}
	return result, nil
}

func (s *leaseScheduler) acquireProvider(ctx context.Context, provider contract.Provider) error {
	gate := s.gates[provider]
	if gate == nil {
		return collecterr.New(collecterr.Failed, "provider gate is not configured")
	}
	select {
	case gate <- struct{}{}:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
}

func (s *leaseScheduler) releaseProvider(provider contract.Provider) {
	gate := s.gates[provider]
	if gate == nil {
		return
	}
	select {
	case <-gate:
	default:
	}
}

func (s *leaseScheduler) observePublished(output collectutil.RunOutput, result sourceobservation.PublishBatchResult) {
	for i, envelope := range output.Observations {
		outcome := outcomeInserted
		if i < len(result.Results) {
			switch result.Results[i].Outcome {
			case sourceobservation.PublishDuplicate:
				outcome = outcomeDuplicate
			case sourceobservation.PublishCollision:
				outcome = outcomeCollision
			}
		}
		s.metrics.ObservePublish(envelope.Provider, string(envelope.ObservationKind), outcome)
		s.metrics.ObserveCompleteness(envelope.Provider, string(envelope.ObservationKind), envelope.Completeness, envelope.Continuity)
	}
}

func (s *leaseScheduler) observeRejected(provider contract.Provider, output collectutil.RunOutput) {
	for _, envelope := range output.Observations {
		s.metrics.ObservePublish(provider, string(envelope.ObservationKind), outcomeRejected)
	}
}

func attemptResult(err error) string {
	if err == nil {
		return resultSuccess
	}
	switch collecterr.Code(err) {
	case collecterr.Timeout:
		return resultTimeout
	case collecterr.Canceled:
		return resultCanceled
	case collecterr.ParserDrift:
		return resultParserDrift
	case collecterr.PaginationGap:
		return resultPaginationGap
	default:
		return resultFailed
	}
}

func (s *leaseScheduler) markQueued(jobKey string) bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, exists := s.queued[jobKey]; exists {
		return false
	}
	s.queued[jobKey] = struct{}{}
	return true
}

func (s *leaseScheduler) unmarkQueued(jobKey string) {
	s.mu.Lock()
	delete(s.queued, jobKey)
	s.mu.Unlock()
}

func (s *leaseScheduler) retryAt(err error) time.Time {
	now := time.Now().UTC()
	minAt := now.Add(s.config.MinRetryDelay)
	maxAt := now.Add(s.config.MaxRetryDelay)
	if retryAt, ok := collecterr.RetryAt(err); ok {
		if retryAt.Before(minAt) {
			return minAt
		}
		if retryAt.After(maxAt) {
			return maxAt
		}
		return retryAt.UTC()
	}
	return now.Add(s.config.MinRetryDelay + (s.config.MaxRetryDelay-s.config.MinRetryDelay)/2)
}

func (s *leaseScheduler) logFailure(phase, code string, spec joblease.JobSpec, proof contract.LeaseProof) {
	if s.logger == nil {
		return
	}
	s.logger.Warn("YouTube collection job failed",
		slog.String("job_key", spec.JobKey),
		slog.String("provider", string(spec.Provider)),
		slog.String("job_kind", spec.CollectionJobKind),
		slog.String("subject_key", spec.SubjectKey),
		slog.Int64("fence_epoch", proof.FenceEpoch),
		slog.Int64("projection_generation", proof.ProjectionGeneration),
		slog.String("error_code", code),
		slog.String("phase", phase),
	)
}
