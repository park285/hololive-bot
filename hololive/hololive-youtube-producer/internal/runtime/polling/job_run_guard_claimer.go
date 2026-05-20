package polling

import (
	"context"
	"fmt"
	"time"

	"github.com/kapu/hololive-shared/pkg/config"
	"github.com/kapu/hololive-shared/pkg/service/cache"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller"
	"github.com/kapu/hololive-youtube-producer/internal/runtime/ingestionlease"
)

func BuildJobRunGuardClaimer(cacheSvc cache.Client, cfg config.ScraperActiveActiveConfig) (poller.JobClaimer, error) {
	if !cfg.Enabled {
		return nil, nil
	}
	if cacheSvc == nil {
		return nil, fmt.Errorf("active-active job run guard requires cache service")
	}
	guard := ingestionlease.NewJobRunGuard(cacheSvc, ingestionlease.JobRunGuardConfig{
		Namespace:  cfg.Namespace,
		InstanceID: cfg.InstanceID,
	})
	return jobRunGuardClaimer{guard: guard}, nil
}

type jobRunGuardClaimer struct {
	guard *ingestionlease.JobRunGuard
}

func (c jobRunGuardClaimer) TryClaim(
	ctx context.Context,
	pollerName string,
	channelID string,
	leaseTTL time.Duration,
	cooldownTTL time.Duration,
) (poller.JobClaimStatus, poller.JobClaim, error) {
	status, claim, err := c.guard.TryClaim(ctx, ingestionlease.JobIdentity{
		PollerName: pollerName,
		ChannelID:  channelID,
		Interval:   cooldownTTL,
	}, leaseTTL, cooldownTTL)
	mapped := poller.JobClaimStatus{
		Result:     mapJobClaimResult(status.Result),
		RetryAfter: status.RetryAfter,
		LeaseTTL:   status.LeaseTTL,
		OwnerToken: status.OwnerToken,
	}
	if claim == nil {
		return mapped, nil, err
	}
	return mapped, jobRunGuardClaim{claim: claim}, err
}

type jobRunGuardClaim struct {
	claim *ingestionlease.JobRunClaim
}

func (c jobRunGuardClaim) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	return c.claim.Renew(ctx, ttl)
}

func (c jobRunGuardClaim) MarkCompleted(ctx context.Context, cooldownTTL time.Duration) (bool, error) {
	return c.claim.MarkCompleted(ctx, cooldownTTL)
}

func (c jobRunGuardClaim) Release(ctx context.Context) (bool, error) {
	return c.claim.Release(ctx)
}

func mapJobClaimResult(result ingestionlease.JobClaimResult) poller.JobClaimResult {
	switch result {
	case ingestionlease.JobClaimAcquired:
		return poller.JobClaimAcquired
	case ingestionlease.JobClaimPeerOwned:
		return poller.JobClaimPeerOwned
	case ingestionlease.JobClaimAlreadyCompleted:
		return poller.JobClaimAlreadyCompleted
	default:
		return poller.JobClaimUnavailable
	}
}
