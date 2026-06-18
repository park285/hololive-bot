package alarm

import (
	"context"
	"net/http"
	"testing"
)

func TestClient_UpdateAlarmAdvanceMinutes_NilContextSkipsRequest(t *testing.T) {
	t.Parallel()

	var requestCount int
	mux := http.NewServeMux()
	mux.HandleFunc("/internal/alarm/settings", func(w http.ResponseWriter, r *http.Request) {
		requestCount++
		w.WriteHeader(http.StatusOK)
	})
	client := newTestClient(t, mux)

	var nilCtx context.Context

	got := client.UpdateAlarmAdvanceMinutes(nilCtx, 5)
	if len(got) != 0 {
		t.Fatalf("len(got) = %d, want 0", len(got))
	}
	if requestCount != 0 {
		t.Fatalf("requestCount = %d, want 0", requestCount)
	}
}
