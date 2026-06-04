package polling

import (
	"context"
	"fmt"
	"strconv"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
)

type GlobalBudgetLimiterConfig struct {
	Namespace          string
	InstanceID         string
	SourceMaxInflight  map[poller.BudgetSource]int
	ClassMaxInflight   map[poller.BudgetBurstClass]int
	DeniedRetryAfter   time.Duration
	WindowCheckEnabled bool
}

type globalBudgetLimiter struct {
	cacheClient        cache.Client
	namespace          string
	instanceID         string
	sourceMaxInflight  map[poller.BudgetSource]int
	classMaxInflight   map[poller.BudgetBurstClass]int
	deniedRetryAfter   time.Duration
	windowCheckEnabled bool
}

type globalBudgetReservation struct {
	cacheClient cache.Client
	namespace   string
	ownerToken  string
	sources     []poller.BudgetSource
	state       atomic.Uint32
}

const (
	defaultGlobalBudgetDeniedRetryAfter = 5 * time.Second

	globalBudgetReservationActive     uint32 = 0
	globalBudgetReservationInProgress uint32 = 1
	globalBudgetReservationDone       uint32 = 2
)

func NewGlobalBudgetLimiter(cacheClient cache.Client, cfg GlobalBudgetLimiterConfig) (poller.GlobalBudgetLimiter, error) {
	if cacheClient == nil {
		return nil, fmt.Errorf("new global budget limiter: cache service must not be nil")
	}
	namespace := normalizeGlobalBudgetNamespace(cfg.Namespace)
	if namespace == "" {
		return nil, fmt.Errorf("new global budget limiter: namespace must not be empty")
	}
	deniedRetryAfter := cfg.DeniedRetryAfter
	if deniedRetryAfter <= 0 {
		deniedRetryAfter = defaultGlobalBudgetDeniedRetryAfter
	}
	return &globalBudgetLimiter{
		cacheClient:        cacheClient,
		namespace:          namespace,
		instanceID:         normalizeGlobalBudgetInstanceID(cfg.InstanceID),
		sourceMaxInflight:  copySourceMaxInflight(cfg.SourceMaxInflight),
		classMaxInflight:   copyClassMaxInflight(cfg.ClassMaxInflight),
		deniedRetryAfter:   deniedRetryAfter,
		windowCheckEnabled: cfg.WindowCheckEnabled,
	}, nil
}

func (l *globalBudgetLimiter) TryReserve(
	ctx context.Context,
	job poller.BudgetJob,
	profile poller.BudgetProfile,
	ttl time.Duration,
) (poller.BudgetReservation, poller.BudgetDecision, error) {
	if len(profile.SourceUnits) == 0 {
		return nil, poller.BudgetDecision{Allowed: true}, nil
	}
	if ttl <= 0 {
		return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: ttl must be positive")
	}

	ownerToken, err := l.newOwnerToken(job)
	if err != nil {
		return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: owner token: %w", err)
	}

	acquired, decision, err := l.reserveProfileSources(ctx, profile, ownerToken, ttl)
	if err != nil {
		return nil, poller.BudgetDecision{}, err
	}
	if !decision.Allowed {
		return nil, decision, nil
	}

	return &globalBudgetReservation{
		cacheClient: l.cacheClient,
		namespace:   l.namespace,
		ownerToken:  ownerToken,
		sources:     acquired,
	}, poller.BudgetDecision{Allowed: true}, nil
}

func (l *globalBudgetLimiter) reserveProfileSources(
	ctx context.Context,
	profile poller.BudgetProfile,
	ownerToken string,
	ttl time.Duration,
) ([]poller.BudgetSource, poller.BudgetDecision, error) {
	sources := sortedBudgetSources(profile.SourceUnits)
	acquired := make([]poller.BudgetSource, 0, len(sources))
	nowMS := time.Now().UnixMilli()
	ttlMS := durationMillis(ttl)
	for _, source := range sources {
		decision, err := l.reserveSource(ctx, source, profile, ownerToken, nowMS, ttlMS)
		if err != nil {
			_ = l.releaseSources(ctx, ownerToken, acquired)
			return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: source %s: %w", source, err)
		}
		if !decision.Allowed {
			if rollbackErr := l.releaseSources(ctx, ownerToken, acquired); rollbackErr != nil {
				return nil, poller.BudgetDecision{}, fmt.Errorf("try reserve global budget: rollback source %s: %w", source, rollbackErr)
			}
			return nil, decision, nil
		}
		acquired = append(acquired, source)
	}
	return acquired, poller.BudgetDecision{Allowed: true}, nil
}

func (l *globalBudgetLimiter) reserveSource(
	ctx context.Context,
	source poller.BudgetSource,
	profile poller.BudgetProfile,
	ownerToken string,
	nowMS int64,
	ttlMS int64,
) (poller.BudgetDecision, error) {
	keys := l.keys(source, profile.BurstClass, ownerToken)
	units := profile.SourceUnits[source]
	cmd := l.cacheClient.B().
		Eval().
		Script(globalBudgetReserveScript).
		Numkeys(8).
		Key(
			keys.ClassInflight,
			keys.GlobalInflight,
			keys.Reservations,
			keys.Reservation,
			keys.SourceCooldown,
			keys.PrimaryInflight,
			keys.BackfillInflight,
			keys.FallbackInflight,
		).
		Arg(
			ownerToken,
			string(profile.BurstClass),
			strconv.FormatFloat(units, 'f', -1, 64),
			strconv.FormatInt(nowMS, 10),
			strconv.FormatInt(ttlMS, 10),
			strconv.Itoa(l.sourceMaxInflight[source]),
			strconv.Itoa(l.classMaxInflight[profile.BurstClass]),
			strconv.FormatInt(durationMillis(l.deniedRetryAfter), 10),
			keys.ReservationPrefix,
			keys.BudgetPrefix,
		).
		Build()
	values, err := evalGlobalBudgetArray(ctx, l.cacheClient, cmd, "reserve global budget")
	if err != nil {
		return poller.BudgetDecision{}, err
	}
	return parseGlobalBudgetReserveResult(values)
}

func (r *globalBudgetReservation) Commit(ctx context.Context) error {
	return r.terminal(ctx, "commit global budget")
}

func (r *globalBudgetReservation) Release(ctx context.Context) error {
	return r.terminal(ctx, "release global budget")
}

func (r *globalBudgetReservation) terminal(ctx context.Context, action string) error {
	if r == nil || r.cacheClient == nil {
		return fmt.Errorf("%s: reservation must not be nil", action)
	}
	if !r.state.CompareAndSwap(globalBudgetReservationActive, globalBudgetReservationInProgress) {
		return nil
	}
	err := r.releaseAll(ctx, action)
	if err != nil {
		r.state.Store(globalBudgetReservationActive)
		return err
	}
	r.state.Store(globalBudgetReservationDone)
	return nil
}

func (r *globalBudgetReservation) releaseAll(ctx context.Context, action string) error {
	var firstErr error
	for _, source := range r.sources {
		keys := buildGlobalBudgetKeys(r.namespace, source, poller.BudgetBurstPrimary, r.ownerToken)
		cmd := r.cacheClient.B().
			Eval().
			Script(globalBudgetReleaseScript).
			Numkeys(6).
			Key(
				keys.GlobalInflight,
				keys.Reservations,
				keys.Reservation,
				keys.PrimaryInflight,
				keys.BackfillInflight,
				keys.FallbackInflight,
			).
			Arg(r.ownerToken, keys.BudgetPrefix).
			Build()
		if err := evalGlobalBudgetInt(ctx, r.cacheClient, cmd, action); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: source %s: %w", action, source, err)
		}
	}
	return firstErr
}

func (l *globalBudgetLimiter) releaseSources(ctx context.Context, ownerToken string, sources []poller.BudgetSource) error {
	reservation := globalBudgetReservation{
		cacheClient: l.cacheClient,
		namespace:   l.namespace,
		ownerToken:  ownerToken,
		sources:     sources,
	}
	return reservation.releaseAll(context.WithoutCancel(ctx), "rollback global budget")
}
