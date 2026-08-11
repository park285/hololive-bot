package pollers

import (
	"context"

	polling "github.com/kapu/hololive-shared/pkg/service/youtube/poller/runtime"
	scraper "github.com/kapu/hololive-shared/pkg/service/youtube/scraper/scraping"
)

const metadataResolvePerPoll = 1

type metadataResolveBudget struct {
	pollerName string
	remaining  int
	metrics    *polling.Metrics
}

type metadataResolveBudgetKey struct{}

func withMetadataResolveBudget(ctx context.Context, pollerName string, metrics *polling.Metrics) context.Context {
	return context.WithValue(ctx, metadataResolveBudgetKey{}, &metadataResolveBudget{
		pollerName: pollerName,
		remaining:  metadataResolvePerPoll,
		metrics:    metrics,
	})
}

func takeMetadataResolve(ctx context.Context) bool {
	budget, ok := ctx.Value(metadataResolveBudgetKey{}).(*metadataResolveBudget)
	if !ok || budget == nil {
		return true
	}
	if budget.remaining <= 0 {
		budget.metrics.ObserveMetadataResolve(budget.pollerName, "deferred")
		return false
	}
	budget.remaining--
	budget.metrics.ObserveMetadataResolve(budget.pollerName, "requested")
	return true
}

func VideosWorstCaseRequestUnits() float64 {
	return float64(scraper.FetchPageMaxAttempts * (2 + metadataResolvePerPoll))
}

func ShortsWorstCaseRequestUnits() float64 {
	return float64(scraper.HighFrequencyChannelFetchPolicy.MaxAttempts * (1 + metadataResolvePerPoll))
}
