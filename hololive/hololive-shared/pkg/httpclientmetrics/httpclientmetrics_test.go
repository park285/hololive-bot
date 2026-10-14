package httpclientmetrics

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/prometheus/client_golang/prometheus"
	"github.com/prometheus/client_golang/prometheus/testutil"
)

// 응답 상태 코드별 요청 수와, 응답을 받지 못한 요청의 "error" 분류가 client 라벨 아래에 함께 남아야 오류율을 계산할 수 있다.
// 실패한 전송도 지연 분포에 들어가야 timeout이 지연 꼬리로 드러난다.
func TestInstrumentCountsStatusCodesAndTransportErrors(t *testing.T) {
	t.Parallel()

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/unavailable" {
			w.WriteHeader(http.StatusServiceUnavailable)

			return
		}

		w.WriteHeader(http.StatusOK)
	}))
	t.Cleanup(server.Close)

	reg := prometheus.NewPedanticRegistry()
	client := &http.Client{Transport: newClientMetrics(reg).instrument("holodex", server.Client().Transport)}

	requireGET(t, client, server.URL+"/ok", true)
	requireGET(t, client, server.URL+"/unavailable", true)
	server.Close()
	requireGET(t, client, server.URL+"/closed", false)

	const want = `
# HELP hololive_http_client_requests_total Outbound HTTP requests by client, response status code ("error" when no response was received) and method.
# TYPE hololive_http_client_requests_total counter
hololive_http_client_requests_total{client="holodex",code="200",method="GET"} 1
hololive_http_client_requests_total{client="holodex",code="503",method="GET"} 1
hololive_http_client_requests_total{client="holodex",code="error",method="GET"} 1
`

	if err := testutil.GatherAndCompare(reg, strings.NewReader(want), "hololive_http_client_requests_total"); err != nil {
		t.Fatal(err)
	}

	if got := durationSampleCount(t, reg); got != 3 {
		t.Fatalf("duration observations = %d, want 3 including the transport error", got)
	}
}

func requireGET(t *testing.T, client *http.Client, url string, wantResponse bool) {
	t.Helper()

	req, err := http.NewRequestWithContext(t.Context(), http.MethodGet, url, http.NoBody)
	if err != nil {
		t.Fatalf("create request %s: %v", url, err)
	}

	resp, err := client.Do(req)
	if err != nil {
		if wantResponse {
			t.Fatalf("GET %s: %v", url, err)
		}

		return
	}

	_ = resp.Body.Close()

	if !wantResponse {
		t.Fatalf("GET %s returned %d, want transport error", url, resp.StatusCode)
	}
}

func durationSampleCount(t *testing.T, reg *prometheus.Registry) uint64 {
	t.Helper()

	families, err := reg.Gather()
	if err != nil {
		t.Fatalf("gather: %v", err)
	}

	for _, family := range families {
		if family.GetName() != "hololive_http_client_request_duration_seconds" {
			continue
		}

		if len(family.GetMetric()) != 1 {
			t.Fatalf("duration series = %d, want 1 (client=holodex, method=GET)", len(family.GetMetric()))
		}

		return family.GetMetric()[0].GetHistogram().GetSampleCount()
	}

	t.Fatal("duration histogram was not gathered")

	return 0
}
