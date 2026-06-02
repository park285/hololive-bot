package pollers

import (
	"net/http"

	"github.com/kapu/hololive-shared/pkg/service/youtube/poller/internal"
)

type NotificationRouteRequest = polling.NotificationRouteRequest

var communityShortsDetectedPostsTotal = polling.NewMetrics().CommunityShortsDetectedPostsTotal

type shortsPollerRoundTripFunc func(req *http.Request) (*http.Response, error)

func (f shortsPollerRoundTripFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}
