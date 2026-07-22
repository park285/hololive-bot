package status

import (
	"bytes"
	"context"
	"io"
	"net/http"
	"strings"
	"time"

	"github.com/kapu/hololive-shared/pkg/httpbody"
)

const maxHealthResponseBodyBytes int64 = 64 << 10

type healthResult struct {
	resp      *http.Response
	latencyMS uint64
	measured  bool
	errMsg    string
}

func doHealthGET(ctx context.Context, ec endpointClient, endpoint ServiceEndpoint) healthResult {
	client, errMsg := ec.resolve()
	if client == nil {
		return healthResult{errMsg: errMsg}
	}
	start := time.Now()
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint.URL, "/")+endpoint.HealthPath, http.NoBody)
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	resp, err := client.Do(req) //nolint:bodyclose // ReadAllAndClose가 network body를 모든 반환 경로에서 닫는다.
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	if resp == nil {
		return healthResult{errMsg: "empty response"}
	}
	latency := elapsedMillis(start)
	body, err := httpbody.ReadAllAndClose(resp.Body, maxHealthResponseBodyBytes)
	if err != nil {
		return healthResult{latencyMS: latency, measured: true, errMsg: "read health response body: " + err.Error()}
	}
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		return healthResult{latencyMS: latency, measured: true, errMsg: "status: " + resp.Status}
	}

	// Collector의 가용성 확인과 Hub의 payload decode가 같은 body 상한을 쓰도록
	// 이미 제한한 응답을 memory body로 재구성한다.
	resp.Body = io.NopCloser(bytes.NewReader(body))
	resp.ContentLength = int64(len(body))
	return healthResult{resp: resp, latencyMS: latency, measured: true}
}

func elapsedMillis(start time.Time) uint64 {
	elapsed := time.Since(start)
	if elapsed <= 0 {
		return 0
	}
	return uint64(elapsed / time.Millisecond)
}
