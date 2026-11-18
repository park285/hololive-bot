package holodexprovider

import (
	"context"
	"sync"

	"github.com/park285/shared-go/v2/pkg/panicguard"

	"github.com/kapu/hololive-shared/pkg/panicscope"
)

type orgFetchResult struct {
	Attempted int
	Succeeded int
	Failed    []string
	Canceled  []string
}

func (r orgFetchResult) wasCanceled() bool {
	return len(r.Canceled) > 0
}

func (r orgFetchResult) hasFailures() bool {
	return len(r.Failed) > 0
}

type orgFetchOutcome uint8

const (
	orgFetchSucceeded orgFetchOutcome = iota
	orgFetchFailed
	orgFetchCanceled
)

func runOrgFetches(ctx context.Context, parallelism int, orgs []string, run func(context.Context, string) error) orgFetchResult {
	outcomes := make([]orgFetchOutcome, len(orgs))

	if parallelism <= 1 {
		for i, org := range orgs {
			outcomes[i] = runOrgFetch(ctx, org, run)
		}
	} else {
		runOrgFetchesParallel(ctx, parallelism, orgs, run, outcomes)
	}

	return summarizeOrgFetches(orgs, outcomes)
}

func runOrgFetchesParallel(ctx context.Context, parallelism int, orgs []string, run func(context.Context, string) error, outcomes []orgFetchOutcome) {
	limiter := make(chan struct{}, parallelism)

	var wg sync.WaitGroup

	for i, org := range orgs {
		limiter <- struct{}{}

		wg.Go(func() {
			defer func() { <-limiter }()

			var outcome orgFetchOutcome

			if err := panicguard.RunE(nil, panicscope.BackgroundTask, "holodex-org-fetch", func() error {
				outcome = runOrgFetch(ctx, org, run)

				return nil
			}); err != nil {
				outcome = orgFetchFailed
			}

			outcomes[i] = outcome
		})
	}

	wg.Wait()
}

func runOrgFetch(ctx context.Context, org string, run func(context.Context, string) error) orgFetchOutcome {
	if ctx.Err() != nil {
		return orgFetchCanceled
	}

	if err := run(ctx, org); err != nil {
		if ctx.Err() != nil {
			return orgFetchCanceled
		}

		return orgFetchFailed
	}

	return orgFetchSucceeded
}

func summarizeOrgFetches(orgs []string, outcomes []orgFetchOutcome) orgFetchResult {
	result := orgFetchResult{
		Attempted: len(orgs),
		Failed:    make([]string, 0, len(orgs)),
	}

	for i, outcome := range outcomes {
		switch outcome {
		case orgFetchSucceeded:
			result.Succeeded++
		case orgFetchFailed:
			result.Failed = append(result.Failed, orgs[i])
		case orgFetchCanceled:
			result.Canceled = append(result.Canceled, orgs[i])
		}
	}

	return result
}
