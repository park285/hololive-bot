// Copyright (c) 2025 Kapu
//
// Permission is hereby granted, free of charge, to any person obtaining a copy
// of this software and associated documentation files (the "Software"), to deal
// in the Software without restriction, including without limitation the rights
// to use, copy, modify, merge, publish, distribute, sublicense, and/or sell
// copies of the Software, and to permit persons to whom the Software is
// furnished to do so, subject to the following conditions:
//
// The above copyright notice and this permission notice shall be included in
// all copies or substantial portions of the Software.
//
// THE SOFTWARE IS PROVIDED "AS IS", WITHOUT WARRANTY OF ANY KIND, EXPRESS OR
// IMPLIED, INCLUDING BUT NOT LIMITED TO THE WARRANTIES OF MERCHANTABILITY,
// FITNESS FOR A PARTICULAR PURPOSE AND NONINFRINGEMENT. IN NO EVENT SHALL THE
// AUTHORS OR COPYRIGHT HOLDERS BE LIABLE FOR ANY CLAIM, DAMAGES OR OTHER
// LIABILITY, WHETHER IN AN ACTION OF CONTRACT, TORT OR OTHERWISE, ARISING FROM,
// OUT OF OR IN CONNECTION WITH THE SOFTWARE OR THE USE OR OTHER DEALINGS IN THE
// SOFTWARE.

package polling

import (
	"context"
	"time"
)

type JobClaimResult string

const (
	JobClaimAcquired         JobClaimResult = "acquired"
	JobClaimPeerOwned        JobClaimResult = "peer_owned"
	JobClaimAlreadyCompleted JobClaimResult = "already_completed"
	JobClaimUnavailable      JobClaimResult = "unavailable"
)

type JobClaimStatus struct {
	Result     JobClaimResult
	RetryAfter time.Duration
	LeaseTTL   time.Duration
	OwnerToken string
}

type JobClaim interface {
	Renew(ctx context.Context, ttl time.Duration) (bool, error)
	MarkCompleted(ctx context.Context, cooldownTTL time.Duration) (bool, error)
	Release(ctx context.Context) (bool, error)
}

type JobClaimer interface {
	TryClaim(ctx context.Context, pollerName, channelID string, leaseTTL, cooldownTTL time.Duration) (JobClaimStatus, JobClaim, error)
}
