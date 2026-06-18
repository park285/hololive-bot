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
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, strings.TrimRight(endpoint.URL, "/")+endpoint.HealthPath, http.NoBody)
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	resp, err := client.Do(req)
	if err != nil {
		return healthResult{errMsg: err.Error()}
	}
	if resp == nil {
		return healthResult{errMsg: "empty response"}
	}
	latency := elapsedMillis(start)
	if resp.StatusCode < 200 || resp.StatusCode >= 300 {
		msg := "status: " + resp.Status
		if err := resp.Body.Close(); err != nil {
			return healthResult{latencyMS: latency, measured: true, errMsg: err.Error()}
		}
		return healthResult{latencyMS: latency, measured: true, errMsg: msg}
	}
	return healthResult{resp: resp, latencyMS: latency, measured: true}
}

func elapsedMillis(start time.Time) uint64 {
	elapsed := time.Since(start)
	if elapsed <= 0 {
		return 0
	}
	return uint64(elapsed / time.Millisecond)
}
