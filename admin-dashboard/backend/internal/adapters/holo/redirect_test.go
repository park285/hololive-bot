package holo

import (
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestMutationDoesNotFollowUpstreamRedirectOrForwardCredentials(t *testing.T) {
	var destinationCalls atomic.Int32

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { destinationCalls.Add(1); w.WriteHeader(http.StatusOK) }))
	t.Cleanup(destination.Close)

	var calls atomic.Int32

	upstream := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		http.Redirect(w, req, destination.URL, http.StatusTemporaryRedirect)
	}))
	t.Cleanup(upstream.Close)

	client, err := NewClient(upstream.URL, "synthetic-api-key")
	require.NoError(t, err)
	t.Cleanup(func() { require.NoError(t, client.Close()) })

	err = client.request(t.Context(), http.MethodPost, "/api/holo/settings", nil, []byte(`{"alarmAdvanceMinutes":15}`), http.StatusOK, new(any))
	require.Error(t, err)
	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, destinationCalls.Load())
}
