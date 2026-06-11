package resolver

import (
	"context"
	"time"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
)

type schedulerClaimStub struct {
	status    polling.JobClaimStatus
	claim     *schedulerClaimHandleStub
	err       error
	tryCalls  int
	poller    string
	channelID string
}

func (c *schedulerClaimStub) TryClaim(
	_ context.Context,
	pollerName string,
	channelID string,
	_, _ time.Duration,
) (polling.JobClaimStatus, polling.JobClaim, error) {
	c.tryCalls++
	c.poller = pollerName
	c.channelID = channelID
	if c.claim == nil {
		return c.status, nil, c.err
	}
	return c.status, c.claim, c.err
}

type schedulerClaimHandleStub struct {
	markCompletedCalls  int
	releaseCalls        int
	renewCalls          int
	markCompletedCtxErr error
	releaseCtxErr       error
	renewFn             func(context.Context, time.Duration) (bool, error)
}

func (c *schedulerClaimHandleStub) Renew(ctx context.Context, ttl time.Duration) (bool, error) {
	c.renewCalls++
	if c.renewFn != nil {
		return c.renewFn(ctx, ttl)
	}
	return true, nil
}

func (c *schedulerClaimHandleStub) MarkCompleted(ctx context.Context, _ time.Duration) (bool, error) {
	c.markCompletedCalls++
	c.markCompletedCtxErr = ctx.Err()
	return true, nil
}

func (c *schedulerClaimHandleStub) Release(ctx context.Context) (bool, error) {
	c.releaseCalls++
	c.releaseCtxErr = ctx.Err()
	return true, nil
}
