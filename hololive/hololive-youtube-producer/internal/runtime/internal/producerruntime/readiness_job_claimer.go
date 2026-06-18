package producerruntime

import (
	"context"
	"log/slog"
	"sync"
	"time"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/readiness"
)

const (
	readinessProbePollerName = "__readiness_probe__"
	readinessProbeChannelID  = "__valkey__"
	readinessProbeTTL        = 15 * time.Second
)

type readinessReportingJobClaimer struct {
	inner       poller.JobClaimer
	readiness   *readiness.State
	pauseLogger *pauseTransitionLogger
}

type readinessReportingBudgetLimiter struct {
	inner       poller.GlobalBudgetLimiter
	readiness   *readiness.State
	pauseLogger *pauseTransitionLogger
}

func newReadinessReportingJobClaimer(inner poller.JobClaimer, state *readiness.State) poller.JobClaimer {
	if inner == nil || state == nil {
		return inner
	}
	return readinessReportingJobClaimer{
		inner:       inner,
		readiness:   state,
		pauseLogger: &pauseTransitionLogger{},
	}
}

func newReadinessReportingBudgetLimiter(inner poller.GlobalBudgetLimiter, state *readiness.State) poller.GlobalBudgetLimiter {
	if inner == nil || state == nil {
		return inner
	}
	return readinessReportingBudgetLimiter{
		inner:       inner,
		readiness:   state,
		pauseLogger: &pauseTransitionLogger{},
	}
}

type pauseReporter struct {
	state     *pauseTransitionLogger
	eventName string
	reason    string
}

func (r pauseReporter) MarkUnavailable(attrs ...any) {
	if r.state == nil || !r.state.markPaused() {
		return
	}
	logAttrs := append([]any{slog.String("reason", r.reason)}, attrs...)
	slog.Warn(r.eventName, logAttrs...)
}

func (r pauseReporter) MarkAvailable() {
	if r.state != nil {
		r.state.markAvailable()
	}
}

func (c readinessReportingJobClaimer) TryClaim(
	ctx context.Context,
	pollerName string,
	channelID string,
	leaseTTL time.Duration,
	cooldownTTL time.Duration,
) (status poller.JobClaimStatus, claim poller.JobClaim, err error) {
	status, claim, err = c.inner.TryClaim(ctx, pollerName, channelID, leaseTTL, cooldownTTL)
	if err != nil || status.Result == poller.JobClaimUnavailable {
		return c.markLeaseUnavailable(status, claim, pollerName, err)
	}
	c.markLeaseAvailable(pollerName)
	return status, claim, nil
}

func (c readinessReportingJobClaimer) markLeaseUnavailable(
	status poller.JobClaimStatus,
	claim poller.JobClaim,
	pollerName string,
	err error,
) (statusResult poller.JobClaimStatus, claimResult poller.JobClaim, errResult error) {
	c.readiness.MarkLeaseUnavailable("valkey_unavailable_active_active_fail_closed")
	if status.Result == "" {
		status.Result = poller.JobClaimUnavailable
	}
	c.logLeaseUnavailable(pollerName, err)
	return status, claim, err
}

func (c readinessReportingJobClaimer) leasePauseReporter() pauseReporter {
	return pauseReporter{
		state:     c.pauseLogger,
		eventName: "active_active_paused",
		reason:    "valkey_unavailable_active_active_fail_closed",
	}
}

func (c readinessReportingJobClaimer) logLeaseUnavailable(pollerName string, err error) {
	attrs := []any{slog.String("poller", pollerName)}
	if err != nil {
		attrs = append(attrs, slog.Any("error", err))
	}
	c.leasePauseReporter().MarkUnavailable(attrs...)
}

func (c readinessReportingJobClaimer) markLeaseAvailable(pollerName string) {
	c.readiness.MarkLeaseAvailable()
	c.leasePauseReporter().MarkAvailable()
	slog.Debug("active_active_lease_available", slog.String("poller", pollerName))
}

type pauseTransitionLogger struct {
	mu     sync.Mutex
	paused bool
}

func (l *pauseTransitionLogger) markPaused() bool {
	l.mu.Lock()
	defer l.mu.Unlock()
	if l.paused {
		return false
	}
	l.paused = true
	return true
}

func (l *pauseTransitionLogger) markAvailable() {
	l.mu.Lock()
	defer l.mu.Unlock()
	l.paused = false
}

func (l readinessReportingBudgetLimiter) TryReserve(
	ctx context.Context,
	job *poller.BudgetJob,
	profile poller.BudgetProfile,
	ttl time.Duration,
) (reservation poller.BudgetReservation, decision poller.BudgetDecision, err error) {
	reservation, decision, err = l.inner.TryReserve(ctx, job, profile, ttl)
	if err != nil {
		pollerName := ""
		if job != nil {
			pollerName = job.PollerName
		}
		l.markBudgetBackendUnavailable(pollerName, err)
		return reservation, decision, err
	}
	l.markBudgetBackendAvailable()
	sources := budgetProfileSources(profile)
	if !decision.Allowed {
		denied := sources
		if decision.AffectedSource != "" {
			denied = []string{decision.AffectedSource}
		}
		l.readiness.MarkBudgetAdmissionDenied(decision.Reason, denied)
		return reservation, decision, nil
	}
	l.readiness.ClearBudgetAdmission(sources)
	return reservation, decision, nil
}

func (l readinessReportingBudgetLimiter) budgetPauseReporter() pauseReporter {
	return pauseReporter{
		state:     l.pauseLogger,
		eventName: "global_budget_paused",
		reason:    "valkey_unavailable_global_budget_fail_closed",
	}
}

func (l readinessReportingBudgetLimiter) markBudgetBackendUnavailable(pollerName string, err error) {
	l.readiness.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")
	l.budgetPauseReporter().MarkUnavailable(
		slog.String("poller", pollerName),
		slog.Any("error", err),
	)
}

func (l readinessReportingBudgetLimiter) markBudgetBackendAvailable() {
	l.readiness.MarkBudgetBackendAvailable()
	l.budgetPauseReporter().MarkAvailable()
}

func (l readinessReportingBudgetLimiter) MarkSourceCooldown(ctx context.Context, source poller.BudgetSource, ttl time.Duration, reason string) error {
	reporter, ok := l.inner.(poller.SourceCooldownReporter)
	if !ok {
		return nil
	}
	if err := reporter.MarkSourceCooldown(ctx, source, ttl, reason); err != nil {
		l.readiness.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")
		return err
	}
	l.readiness.MarkBudgetBackendAvailable()
	l.readiness.MarkSourceCooldownFor([]string{string(source)}, ttl)
	return nil
}

func budgetProfileSources(profile poller.BudgetProfile) []string {
	sources := make([]string, 0, len(profile.SourceUnits))
	for source := range profile.SourceUnits {
		sources = append(sources, string(source))
	}
	return sources
}

func probeReadinessJobClaimer(ctx context.Context, claimer poller.JobClaimer, logger *slog.Logger) {
	if claimer == nil {
		return
	}
	status, claim, err := claimer.TryClaim(ctx, readinessProbePollerName, readinessProbeChannelID, readinessProbeTTL, readinessProbeTTL)
	if err != nil {
		logReadinessProbeWarning(logger, "active_active_readiness_probe_failed", err)
		return
	}
	if status.Result == poller.JobClaimAcquired && claim != nil {
		releaseReadinessProbeClaim(ctx, claim, logger)
	}
}

func releaseReadinessProbeClaim(ctx context.Context, claim poller.JobClaim, logger *slog.Logger) {
	if _, err := claim.Release(ctx); err != nil {
		logReadinessProbeWarning(logger, "active_active_readiness_probe_release_failed", err)
	}
}

func logReadinessProbeWarning(logger *slog.Logger, message string, err error) {
	if logger != nil {
		logger.Warn(message, slog.Any("error", err))
	}
}
