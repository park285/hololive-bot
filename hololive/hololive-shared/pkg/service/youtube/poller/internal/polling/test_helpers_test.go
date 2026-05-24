package polling

import (
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/pollertestdb"
)

var testMetrics = NewMetrics()

var (
	publishedAtResolutionAttemptTotal = testMetrics.PublishedAtResolutionAttemptTotal
	publishedAtResolutionSuccessTotal = testMetrics.PublishedAtResolutionSuccessTotal
	publishedAtResolutionFailureTotal = testMetrics.PublishedAtResolutionFailureTotal
	publishedAtResolverSkippedTotal   = testMetrics.PublishedAtResolverSkippedTotal
	publishedAtResolverEnqueuedTotal  = testMetrics.PublishedAtResolverEnqueuedTotal
	publishedAtResolverPageCandidates = testMetrics.PublishedAtResolverPageCandidates
	publishedAtResolverScannedTotal   = testMetrics.PublishedAtResolverScannedTotal
)

type shortsPollerRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f shortsPollerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newBatchTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()

	return pollertestdb.NewBatchTestDB(t, models...)
}
