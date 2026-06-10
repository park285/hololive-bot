package status

import (
	"context"
	"net/http"
	"strings"
	"time"
)

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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint.URL, "/")+endpoint.HealthPath, nil)
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	latency := uint64(time.Since(start).Milliseconds())
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := "status: " + resp.Status
		resp.Body.Close()
		return healthResult{latencyMS: latency, measured: true, errMsg: msg}
	}
	return healthResult{resp: resp, latencyMS: latency, measured: true}
}
