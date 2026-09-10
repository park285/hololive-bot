package httpapi

import (
	"bytes"
	"context"
	"log/slog"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync/atomic"
	"testing"
	"time"

	"github.com/stretchr/testify/require"

	"github.com/kapu/admin-dashboard/internal/contract"
)

func TestDrainFencesRequestsAndPreservesDispatchedUnknown(t *testing.T) {
	entered, release := make(chan struct{}), make(chan struct{})

	var calls atomic.Int32

	r := runtimeWithHolo(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		calls.Add(1)
		close(entered)
		<-release
		w.WriteHeader(http.StatusBadGateway)
	}))

	var audit bytes.Buffer

	r.logger = slog.New(slog.NewJSONHandler(&audit, nil))

	handler := r.Handler()
	req := mutationRequest(t, http.MethodPost, "/admin/api/holo/rooms", `{"room":"fixture"}`)
	req.Header.Set("X-Admin-Client-Generation", contract.Generation)

	done := make(chan *httptest.ResponseRecorder, 1)

	go func() {
		rec := httptest.NewRecorder()
		handler.ServeHTTP(rec, req)

		done <- rec
	}()

	select {
	case <-entered:
	case <-time.After(time.Second):
		close(release)
		t.Fatal("upstream was not called")
	}

	inFlight := r.BeginDrain()
	require.Len(t, inFlight, 1)
	require.Equal(t, "outcome_unknown", inFlight[0].Outcome)
	require.Equal(t, "holoAddRoom", inFlight[0].Operation)
	require.NotEmpty(t, inFlight[0].RequestID)

	ctx, cancel := context.WithCancel(t.Context())
	cancel()
	require.ErrorIs(t, r.WaitDrained(ctx), context.Canceled)

	for _, path := range []string{"/admin/api/holo/rooms", "/admin/api/auth/login", "/admin/api/ws/system-stats"} {
		rec := doRequest(handler, httptest.NewRequestWithContext(t.Context(), http.MethodPost, path, strings.NewReader(`{}`)))
		if strings.Contains(path, "/ws/") {
			rec = doRequest(handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, path, http.NoBody))
		}

		require.Equal(t, http.StatusServiceUnavailable, rec.Code)
		require.Contains(t, rec.Body.String(), "ADMISSION_CLOSED")
	}

	meta := doRequest(handler, httptest.NewRequestWithContext(t.Context(), http.MethodGet, "/admin/meta.json", http.NoBody))
	require.Equal(t, http.StatusOK, meta.Code)
	close(release)

	rec := <-done
	require.Equal(t, http.StatusBadGateway, rec.Code)
	require.NoError(t, r.WaitDrained(t.Context()))
	require.Empty(t, r.BeginDrain())
	require.EqualValues(t, 1, calls.Load())
	require.Contains(t, audit.String(), `"result":"outcome_unknown"`)
}

func TestDrainSnapshotDoesNotInventDispatchOrEffect(t *testing.T) {
	r := &API{logger: slog.New(slog.DiscardHandler)}
	_, ok := r.admission.enter(InFlight{RequestID: "read", Operation: "read"})
	require.True(t, ok)

	_, ok = r.admission.enter(InFlight{RequestID: "mutation", Operation: "change", Mutation: true})
	require.True(t, ok)

	states := r.BeginDrain()
	require.Equal(t, "not_dispatched", states[0].Outcome)
	require.Equal(t, "in_progress", states[1].Outcome)
	r.admission.leave("read")
	r.admission.leave("mutation")
	require.NoError(t, r.WaitDrained(t.Context()))
}
