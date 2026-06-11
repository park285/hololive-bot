package resolver

import (
	"context"
	"fmt"
)

const (
	PendingPublishedAtResolverPollerName          = "pending_published_at_resolver"
	PendingPublishedAtResolverCandidatePollerName = "pending_published_at_candidate"
)

type PendingPublishedAtResolverPoller struct {
	resolver *PendingPublishedAtResolver
}

func NewPendingPublishedAtResolverPoller(resolver *PendingPublishedAtResolver) *PendingPublishedAtResolverPoller {
	if resolver == nil {
		return nil
	}

	return &PendingPublishedAtResolverPoller{
		resolver: resolver,
	}
}

func (p *PendingPublishedAtResolverPoller) Poll(ctx context.Context, _ string) error {
	if p == nil || p.resolver == nil {
		return fmt.Errorf("poll pending published_at resolver: resolver is nil")
	}

	return p.resolver.RunOnce(ctx)
}

func (p *PendingPublishedAtResolverPoller) Name() string {
	return PendingPublishedAtResolverPollerName
}
