package polling

import (
	"context"
	"fmt"
	"strconv"
	"strings"
	"sync/atomic"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/cache"
	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"

	"github.com/kapu/hololive-youtube-producer/internal/runtime/instanceid"
)

type GlobalBudgetLimiterConfig struct {
	Namespace          string
	InstanceID         string
	SourceMaxInflight  map[polling.BudgetSource]int
	ClassMaxInflight   map[polling.BudgetBurstClass]int
	DeniedRetryAfter   time.Duration
	WindowCheckEnabled bool
	// CleanupLimit은 단일 Valkey Lua reserve 실행 안에서 만료된 reservation cleanup
	// 작업량을 제한한다. 0 이하 값이면 defaultGlobalBudgetCleanupLimit을 사용한다.
	CleanupLimit int
}

type globalBudgetLimiter struct {
	cacheClient        cache.Client
	namespace          string
	instanceID         string
	sourceMaxInflight  map[polling.BudgetSource]int
	classMaxInflight   map[polling.BudgetBurstClass]int
	deniedRetryAfter   time.Duration
	windowCheckEnabled bool
	cleanupLimit       int
}

type globalBudgetReservation struct {
	cacheClient       cache.Client
	namespace         string
	ownerToken        string
	reservationMember string
	burstClass        polling.BudgetBurstClass
	sources           []polling.BudgetSource
	state             atomic.Uint32
}

const (
	defaultGlobalBudgetDeniedRetryAfter = 5 * time.Second
	defaultGlobalBudgetCleanupLimit     = 128

	globalBudgetReservationActive     uint32 = 0
	globalBudgetReservationInProgress uint32 = 1
	globalBudgetReservationDone       uint32 = 2
)

func NewGlobalBudgetLimiter(cacheClient cache.Client, cfg GlobalBudgetLimiterConfig) (polling.GlobalBudgetLimiter, error) {
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
	cleanupLimit := cfg.CleanupLimit
	if cleanupLimit <= 0 {
		cleanupLimit = defaultGlobalBudgetCleanupLimit
	}
	return &globalBudgetLimiter{
		cacheClient:        cacheClient,
		namespace:          namespace,
		instanceID:         instanceid.Normalize(cfg.InstanceID),
		sourceMaxInflight:  copySourceMaxInflight(cfg.SourceMaxInflight),
		classMaxInflight:   copyClassMaxInflight(cfg.ClassMaxInflight),
		deniedRetryAfter:   deniedRetryAfter,
		windowCheckEnabled: cfg.WindowCheckEnabled,
		cleanupLimit:       cleanupLimit,
	}, nil
}

func (l *globalBudgetLimiter) TryReserve(
	ctx context.Context,
	job *polling.BudgetJob,
	profile polling.BudgetProfile,
	ttl time.Duration,
) (reservation polling.BudgetReservation, decision polling.BudgetDecision, err error) {
	if len(profile.SourceUnits) == 0 {
		return nil, polling.BudgetDecision{Allowed: true}, nil
	}
	if ttl <= 0 {
		return nil, polling.BudgetDecision{}, fmt.Errorf("try reserve global budget: ttl must be positive")
	}
	if job == nil {
		return nil, polling.BudgetDecision{}, fmt.Errorf("try reserve global budget: job must not be nil")
	}
	profile = normalizeGlobalBudgetProfile(profile)
	return l.tryReserveNormalizedProfile(ctx, job, profile, ttl)
}

func (l *globalBudgetLimiter) tryReserveNormalizedProfile(
	ctx context.Context,
	job *polling.BudgetJob,
	profile polling.BudgetProfile,
	ttl time.Duration,
) (reservation polling.BudgetReservation, decision polling.BudgetDecision, err error) {
	if decision, denied, cooldownErr := l.fallbackSourceCooldownDecision(ctx, profile); cooldownErr != nil {
		return nil, polling.BudgetDecision{}, cooldownErr
	} else if denied {
		return nil, decision, nil
	}
	ownerToken, err := l.newOwnerToken(job)
	if err != nil {
		return nil, polling.BudgetDecision{}, fmt.Errorf("try reserve global budget: owner token: %w", err)
	}
	reservationMember := globalBudgetReservationMember(profile.BurstClass, ownerToken)

	acquired, decision, err := l.reserveProfileSources(ctx, profile, ownerToken, reservationMember, ttl)
	if err != nil {
		return nil, polling.BudgetDecision{}, err
	}
	if !decision.Allowed {
		return nil, decision, nil
	}

	return &globalBudgetReservation{
		cacheClient:       l.cacheClient,
		namespace:         l.namespace,
		ownerToken:        ownerToken,
		reservationMember: reservationMember,
		burstClass:        profile.BurstClass,
		sources:           acquired,
	}, polling.BudgetDecision{Allowed: true}, nil
}

func normalizeGlobalBudgetProfile(profile polling.BudgetProfile) polling.BudgetProfile {
	if strings.TrimSpace(string(profile.BurstClass)) == "" {
		profile.BurstClass = polling.BudgetBurstPrimary
	}
	return profile
}

func (l *globalBudgetLimiter) MarkSourceCooldown(ctx context.Context, source polling.BudgetSource, ttl time.Duration, reason string) error {
	if l == nil || l.cacheClient == nil {
		return fmt.Errorf("mark source cooldown: limiter is nil")
	}
	if strings.TrimSpace(string(source)) == "" {
		return fmt.Errorf("mark source cooldown: source must not be empty")
	}
	if ttl <= 0 {
		return nil
	}

	keys := buildGlobalBudgetKeys(l.namespace, source, polling.BudgetBurstPrimary, "")
	value := globalBudgetSourceCooldownValue(l.instanceID, reason)
	cmd := l.cacheClient.B().
		Set().
		Key(keys.SourceCooldown).
		Value(value).
		ExSeconds(globalBudgetCooldownSeconds(ttl)).
		Build()

	client := l.cacheClient.GetClient()
	if client == nil {
		return fmt.Errorf("mark source cooldown %s: cache client is nil", source)
	}
	if err := client.Do(ctx, cmd).Error(); err != nil {
		return fmt.Errorf("mark source cooldown %s: %w", source, err)
	}
	return nil
}

func globalBudgetSourceCooldownValue(instanceID, reason string) string {
	trimmedReason := strings.TrimSpace(reason)
	if trimmedReason == "" {
		trimmedReason = "source_cooldown"
	}
	return instanceid.Normalize(instanceID) + ":" + trimmedReason
}

func globalBudgetCooldownSeconds(ttl time.Duration) int64 {
	seconds := int64(ttl / time.Second)
	if ttl%time.Second != 0 {
		seconds++
	}
	if seconds <= 0 {
		return 1
	}
	return seconds
}

func (l *globalBudgetLimiter) reserveProfileSources(
	ctx context.Context,
	profile polling.BudgetProfile,
	ownerToken string,
	reservationMember string,
	ttl time.Duration,
) (acquiredSources []polling.BudgetSource, decision polling.BudgetDecision, err error) {
	sources := sortedBudgetSources(profile.SourceUnits)
	acquired := make([]polling.BudgetSource, 0, len(sources))
	nowMS := time.Now().UnixMilli()
	ttlMS := durationMillis(ttl)
	for _, source := range sources {
		decision, err := l.reserveSource(ctx, source, profile, reservationMember, nowMS, ttlMS)
		if err != nil {
			return nil, polling.BudgetDecision{}, l.wrapReserveSourceError(ctx, ownerToken, reservationMember, profile.BurstClass, acquired, source, err)
		}
		if !decision.Allowed {
			decision, err = l.rollbackDeniedReserveSource(ctx, ownerToken, reservationMember, profile.BurstClass, acquired, source, decision)
			if err != nil {
				return nil, polling.BudgetDecision{}, err
			}
			return nil, decision, nil
		}
		acquired = append(acquired, source)
	}
	return acquired, polling.BudgetDecision{Allowed: true}, nil
}

func (l *globalBudgetLimiter) wrapReserveSourceError(
	ctx context.Context,
	ownerToken string,
	reservationMember string,
	burstClass polling.BudgetBurstClass,
	acquired []polling.BudgetSource,
	source polling.BudgetSource,
	err error,
) error {
	if rollbackErr := l.releaseSourcesForClass(ctx, ownerToken, reservationMember, burstClass, acquired); rollbackErr != nil {
		return fmt.Errorf("try reserve global budget: source %s: %w; rollback: %w", source, err, rollbackErr)
	}
	return fmt.Errorf("try reserve global budget: source %s: %w", source, err)
}

func (l *globalBudgetLimiter) rollbackDeniedReserveSource(
	ctx context.Context,
	ownerToken string,
	reservationMember string,
	burstClass polling.BudgetBurstClass,
	acquired []polling.BudgetSource,
	source polling.BudgetSource,
	decision polling.BudgetDecision,
) (polling.BudgetDecision, error) {
	if rollbackErr := l.releaseSourcesForClass(ctx, ownerToken, reservationMember, burstClass, acquired); rollbackErr != nil {
		return polling.BudgetDecision{}, fmt.Errorf("try reserve global budget: rollback source %s: %w", source, rollbackErr)
	}
	decision.AffectedSource = string(source)
	return decision, nil
}

func (l *globalBudgetLimiter) reserveSource(
	ctx context.Context,
	source polling.BudgetSource,
	profile polling.BudgetProfile,
	reservationMember string,
	nowMS int64,
	ttlMS int64,
) (polling.BudgetDecision, error) {
	keys := l.keys(source, profile.BurstClass, reservationMember)
	units := profile.SourceUnits[source]
	keysForScript := []string{
		keys.ClassInflight,
		keys.GlobalInflight,
		keys.Reservations,
		keys.Reservation,
		keys.SourceCooldown,
		keys.PrimaryInflight,
		keys.BackfillInflight,
		keys.FallbackInflight,
	}
	args := []string{
		reservationMember,
		string(profile.BurstClass),
		strconv.FormatFloat(units, 'f', -1, 64),
		strconv.FormatInt(nowMS, 10),
		strconv.FormatInt(ttlMS, 10),
		strconv.Itoa(l.sourceMaxInflight[source]),
		strconv.Itoa(l.classMaxInflight[profile.BurstClass]),
		strconv.FormatInt(durationMillis(l.deniedRetryAfter), 10),
		keys.ReservationPrefix,
		keys.BudgetPrefix,
		strconv.Itoa(l.cleanupLimit),
	}
	values, err := evalGlobalBudgetArray(ctx, l.cacheClient, globalBudgetReserveLua, keysForScript, args, "reserve global budget")
	if err != nil {
		return polling.BudgetDecision{}, err
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
	class := r.burstClass
	if strings.TrimSpace(string(class)) == "" {
		class = polling.BudgetBurstPrimary
	}
	reservationMember := r.reservationMember
	if reservationMember == "" {
		reservationMember = globalBudgetReservationMember(class, r.ownerToken)
	}
	var firstErr error
	for _, source := range r.sources {
		keys := buildGlobalBudgetKeys(r.namespace, source, class, reservationMember)
		keysForScript := []string{
			keys.GlobalInflight,
			keys.Reservations,
			keys.Reservation,
			keys.PrimaryInflight,
			keys.BackfillInflight,
			keys.FallbackInflight,
		}
		args := []string{reservationMember, keys.BudgetPrefix, string(class)}
		if err := evalGlobalBudgetInt(ctx, r.cacheClient, globalBudgetReleaseLua, keysForScript, args, action); err != nil && firstErr == nil {
			firstErr = fmt.Errorf("%s: source %s: %w", action, source, err)
		}
	}
	return firstErr
}

func (l *globalBudgetLimiter) releaseSourcesForClass(
	ctx context.Context,
	ownerToken string,
	reservationMember string,
	class polling.BudgetBurstClass,
	sources []polling.BudgetSource,
) error {
	reservation := globalBudgetReservation{
		cacheClient:       l.cacheClient,
		namespace:         l.namespace,
		ownerToken:        ownerToken,
		reservationMember: reservationMember,
		burstClass:        class,
		sources:           sources,
	}
	return reservation.releaseAll(context.WithoutCancel(ctx), "rollback global budget")
}
