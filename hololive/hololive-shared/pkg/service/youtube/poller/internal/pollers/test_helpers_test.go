package pollers

import (
	"net/http"
	"testing"

	"gorm.io/gorm"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal/pollertestdb"
)

type NotificationRouteRequest = polling.NotificationRouteRequest

var communityShortsDetectedPostsTotal = polling.NewMetrics().CommunityShortsDetectedPostsTotal

type shortsPollerRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f shortsPollerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func newBatchTestDB(t *testing.T, models ...any) *gorm.DB {
	t.Helper()

	return pollertestdb.NewBatchTestDB(t, models...)
}
