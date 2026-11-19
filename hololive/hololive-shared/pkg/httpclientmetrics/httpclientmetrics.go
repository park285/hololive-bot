package httpclientmetrics

import (
	"cmp"
	"errors"
	"net/http"
	"strconv"
	"sync"
	"time"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/promauto"
)

const codeTransportError = "error"

type clientMetrics struct {
	requests *prometheus.CounterVec
	duration *prometheus.HistogramVec
}

func newClientMetrics(reg prometheus.Registerer) *clientMetrics {
	factory := promauto.With(reg)

	return &clientMetrics{
		requests: factory.NewCounterVec(prometheus.CounterOpts{
			Name: "hololive_http_client_requests_total",
			Help: "Outbound HTTP requests by client, response status code (\"error\" when no response was received) and method.",
		}, []string{"client", "code", "method"}),
		duration: factory.NewHistogramVec(prometheus.HistogramOpts{
			Name:    "hololive_http_client_request_duration_seconds",
			Help:    "Time from sending an outbound HTTP request until response headers arrive or the transport fails.",
			Buckets: []float64{0.05, 0.1, 0.25, 0.5, 1, 2.5, 5, 10, 30},
		}, []string{"client", "method"}),
	}
}

var defaultMetrics = sync.OnceValue(func() *clientMetrics {
	return newClientMetrics(prometheus.DefaultRegisterer)
})

func Instrument(client string, next http.RoundTripper) http.RoundTripper {
	return defaultMetrics().instrument(client, next)
}

func (m *clientMetrics) instrument(client string, next http.RoundTripper) http.RoundTripper {
	labels := prometheus.Labels{"client": client}

	return &instrumentedTransport{
		next:     next,
		requests: m.requests.MustCurryWith(labels),
		duration: m.duration.MustCurryWith(labels),
	}
}

var errNilResponse = errors.New("round trip returned neither response nor error")

type instrumentedTransport struct {
	next     http.RoundTripper
	requests *prometheus.CounterVec
	duration prometheus.ObserverVec
}

func (t *instrumentedTransport) RoundTrip(req *http.Request) (*http.Response, error) {
	start := time.Now()
	resp, err := t.next.RoundTrip(req)
	method := cmp.Or(req.Method, http.MethodGet)

	t.duration.WithLabelValues(method).Observe(time.Since(start).Seconds())

	if err != nil {
		t.requests.WithLabelValues(codeTransportError, method).Inc()

		return nil, err
	}

	if resp == nil {
		t.requests.WithLabelValues(codeTransportError, method).Inc()

		return nil, errNilResponse
	}

	t.requests.WithLabelValues(strconv.Itoa(resp.StatusCode), method).Inc()

	return resp, nil
}
