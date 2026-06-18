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

func BuildJobRunGuardClaimer(cacheClient cache.Client, activeActiveConfig config.ScraperActiveActiveConfig) (poller.JobClaimer, error) {
	if !activeActiveConfig.Enabled {
		return nil, nil
	}
	if cacheClient == nil {
		return nil, fmt.Errorf("active-active job run guard requires cache service")
	}
	guard := ingestionlease.NewJobRunGuard(cacheClient, ingestionlease.JobRunGuardConfig{
		Namespace:  activeActiveConfig.Namespace,
		InstanceID: activeActiveConfig.InstanceID,
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
) (statusResult poller.JobClaimStatus, claimResult poller.JobClaim, err error) {
	if c.guard == nil {
		return poller.JobClaimStatus{Result: poller.JobClaimUnavailable}, nil, fmt.Errorf("job run guard is nil")
	}
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

func (c jobRunGuardClaim) Defer(ctx context.Context, retryAfter time.Duration) (bool, error) {
	return c.claim.Defer(ctx, retryAfter)
}

func (c jobRunGuardClaim) Release(ctx context.Context) (bool, error) {
	return c.claim.Release(ctx)
}

var jobClaimResultMap = map[ingestionlease.JobClaimResult]poller.JobClaimResult{
	ingestionlease.JobClaimAcquired:         poller.JobClaimAcquired,
	ingestionlease.JobClaimPeerOwned:        poller.JobClaimPeerOwned,
	ingestionlease.JobClaimAlreadyCompleted: poller.JobClaimAlreadyCompleted,
	ingestionlease.JobClaimUnavailable:      poller.JobClaimUnavailable,
}

func mapJobClaimResult(result ingestionlease.JobClaimResult) poller.JobClaimResult {
	mapped, ok := jobClaimResultMap[result]
	if !ok {
		return poller.JobClaimUnavailable
	}
	return mapped
}
