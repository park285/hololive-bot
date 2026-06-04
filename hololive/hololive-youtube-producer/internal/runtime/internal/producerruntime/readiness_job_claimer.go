package producerruntime

import (
	"context"
	"fmt"
	"log/slog"
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
	inner     poller.JobClaimer
	readiness *readiness.State
}

type readinessReportingBudgetLimiter struct {
	inner     poller.GlobalBudgetLimiter
	readiness *readiness.State
}

func newReadinessReportingJobClaimer(inner poller.JobClaimer, state *readiness.State) poller.JobClaimer {
	if inner == nil || state == nil {
		return inner
	}
	return readinessReportingJobClaimer{
		inner:     inner,
		readiness: state,
	}
}

func newReadinessReportingBudgetLimiter(inner poller.GlobalBudgetLimiter, state *readiness.State) poller.GlobalBudgetLimiter {
	if inner == nil || state == nil {
		return inner
	}
	return readinessReportingBudgetLimiter{
		inner:     inner,
		readiness: state,
	}
}

func (c readinessReportingJobClaimer) TryClaim(
	ctx context.Context,
	pollerName string,
	channelID string,
	leaseTTL time.Duration,
	cooldownTTL time.Duration,
) (poller.JobClaimStatus, poller.JobClaim, error) {
	status, claim, err := c.inner.TryClaim(ctx, pollerName, channelID, leaseTTL, cooldownTTL)
	if err != nil || status.Result == poller.JobClaimUnavailable {
		c.readiness.MarkLeaseUnavailable("valkey_unavailable_active_active_fail_closed")
		slog.Warn("active_active_paused",
			slog.String("reason", "valkey_unavailable_active_active_fail_closed"),
			slog.String("poller", pollerName),
		)
		if err != nil {
			return status, claim, err
		}
		return status, claim, fmt.Errorf("job lease unavailable")
	}
	c.readiness.MarkLeaseAvailable()
	slog.Debug("active_active_lease_available", slog.String("poller", pollerName))
	return status, claim, nil
}

func (l readinessReportingBudgetLimiter) TryReserve(
	ctx context.Context,
	job poller.BudgetJob,
	profile poller.BudgetProfile,
	ttl time.Duration,
) (poller.BudgetReservation, poller.BudgetDecision, error) {
	reservation, decision, err := l.inner.TryReserve(ctx, job, profile, ttl)
	if err != nil {
		l.readiness.MarkBudgetBackendUnavailable("valkey_unavailable_global_budget_fail_closed")
		slog.Warn("global_budget_paused",
			slog.String("reason", "valkey_unavailable_global_budget_fail_closed"),
			slog.String("poller", job.PollerName),
			slog.Any("error", err),
		)
		return reservation, decision, err
	}
	l.readiness.MarkBudgetBackendAvailable()
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
