// Package httpclientmetrics는 외부 HTTP 호출의 요청 수·상태 코드·지연을 client 라벨별로 기록한다.
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

// codeTransportError는 응답을 받지 못한 요청(연결 거부·timeout·취소 등)의 code 라벨이다.
// Prometheus의 promhttp 계측기는 이런 요청을 세지 않아 오류율의 분모와 분자가 모두 빠지므로 직접 기록한다.
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

// 한 프로세스의 여러 client가 같은 메트릭을 공유하므로 기본 registry에는 한 번만 등록한다.
var defaultMetrics = sync.OnceValue(func() *clientMetrics {
	return newClientMetrics(prometheus.DefaultRegisterer)
})

// Instrument는 next를 감싸 요청마다 기본 registry의 요청 수와 지연을 client 라벨로 기록한다.
// 지연은 응답 헤더 도착(또는 전송 실패)까지이며 본문 읽기 시간은 포함하지 않는다.
// 감쌀 RoundTripper는 nil이 아니어야 하며, 반환한 RoundTripper는 그 오류와 응답을 바꾸지 않는다.
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
	// http.Request는 빈 Method를 GET으로 보낸다.
	method := cmp.Or(req.Method, http.MethodGet)

	t.duration.WithLabelValues(method).Observe(time.Since(start).Seconds())

	if err != nil {
		t.requests.WithLabelValues(codeTransportError, method).Inc()

		return nil, err
	}

	// RoundTripper 계약상 err가 nil이면 resp가 있다. 계약을 어긴 transport는 nil 응답을 넘기지 않고 오류로 드러낸다.
	if resp == nil {
		t.requests.WithLabelValues(codeTransportError, method).Inc()

		return nil, errNilResponse
	}

	t.requests.WithLabelValues(strconv.Itoa(resp.StatusCode), method).Inc()

	return resp, nil
}
