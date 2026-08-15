package main

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"github.com/kapu/hololive-shared/pkg/contracts/common"
)

func TestFetchBodyWithAPIKeyEnvSendsAPIKeyHeader(t *testing.T) {
	t.Setenv("PROBE_API_KEY", "probe-secret")

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if got := r.Header.Get(common.APIKeyHeader); got != "probe-secret" {
			w.WriteHeader(http.StatusUnauthorized)
			return
		}
		if _, err := w.Write([]byte(`metrics body`)); err != nil {
			t.Errorf("write response: %v", err)
		}
	}))
	defer server.Close()

	body, err := fetchBodyWithAPIKeyEnv("PROBE_API_KEY", server.URL)
	if err != nil {
		t.Fatalf("fetchBodyWithAPIKeyEnv() error = %v", err)
	}
	if string(body) != "metrics body" {
		t.Fatalf("body = %q, want metrics body", body)
	}
}

func TestFetchBodyWithAPIKeyEnvRejectsMissingEnv(t *testing.T) {
	t.Setenv("PROBE_API_KEY", "")

	_, err := fetchBodyWithAPIKeyEnv("PROBE_API_KEY", "http://127.0.0.1/metrics")
	if err == nil {
		t.Fatal("fetchBodyWithAPIKeyEnv() error = nil, want missing env error")
	}
	if !strings.Contains(err.Error(), "PROBE_API_KEY") {
		t.Fatalf("error = %q, want env name", err.Error())
	}
}
