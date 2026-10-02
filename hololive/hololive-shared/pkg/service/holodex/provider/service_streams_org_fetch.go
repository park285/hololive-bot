package holodexprovider

import (
	"context"
	"sync"

	"github.com/park285/shared-go/v2/pkg/panicguard"
)

// orgFetchResult는 org별 Holodex primary 조회 결과다. 호출자 취소로 끝난 org는 원천 실패가 아니므로
// Failed가 아니라 Canceled에 담는다. 실패로 세면 취소된 요청이 공식 일정 fallback·재시도·실패 metric을 일으킨다.
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

// runOrgFetches는 org마다 run을 최대 parallelism개씩 동시에 실행하고 결과를 입력 순서대로 모은다.
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

			// panic은 org 실패로 센다. panicguard가 복구 사실을 로그로 남긴다.
			if err := panicguard.RunE(nil, panicguard.BackgroundTask, "holodex-org-fetch", func() error {
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
