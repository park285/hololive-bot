package testutil

import (
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"testing"
)

func NewJSONTestServer(t *testing.T, statusCode int, responseBody any, assertFn func(r *http.Request)) *httptest.Server {
	t.Helper()

	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if assertFn != nil {
			assertFn(r)
		}

		w.Header().Set("Content-Type", "application/json")
		w.WriteHeader(statusCode)

		if responseBody != nil {
			if err := json.NewEncoder(w).Encode(responseBody); err != nil {
				t.Errorf("encode JSON response: %v", err)
			}
		}
	}))

	t.Cleanup(srv.Close)

	return srv
}
