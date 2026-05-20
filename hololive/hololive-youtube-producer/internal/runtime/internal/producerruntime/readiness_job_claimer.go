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

func newReadinessReportingJobClaimer(inner poller.JobClaimer, state *readiness.State) poller.JobClaimer {
	if inner == nil || state == nil {
		return inner
	}
	return readinessReportingJobClaimer{
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

func probeReadinessJobClaimer(ctx context.Context, claimer poller.JobClaimer, logger *slog.Logger) {
	if claimer == nil {
		return
	}
	status, claim, err := claimer.TryClaim(ctx, readinessProbePollerName, readinessProbeChannelID, readinessProbeTTL, readinessProbeTTL)
	if err != nil {
		if logger != nil {
			logger.Warn("active_active_readiness_probe_failed", slog.Any("error", err))
		}
		return
	}
	if status.Result == poller.JobClaimAcquired && claim != nil {
		if _, err := claim.Release(ctx); err != nil && logger != nil {
			logger.Warn("active_active_readiness_probe_release_failed", slog.Any("error", err))
		}
	}
}
