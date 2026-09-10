package docker

import (
	"context"
	"fmt"
	"net/http"
	"net/http/httptest"
	"sync/atomic"
	"testing"

	"github.com/stretchr/testify/require"
)

func TestActionsRequireEngineCompletionWithoutReplay(t *testing.T) {
	// Docker Engine v1.52의 독립 응답 표: 일반 2xx·restart의 304는 완료 증거가 아닙니다.
	for _, tc := range []struct {
		action    string
		status    int
		confirmed bool
	}{
		{testStartAction, 204, true},
		{testStartAction, 304, true},
		{testStartAction, 200, false},
		{testStartAction, 202, false},
		{testStopAction, 204, true},
		{testStopAction, 304, true},
		{testStopAction, 200, false},
		{testStopAction, 202, false},
		{testRestartAction, 204, true},
		{testRestartAction, 304, false},
		{testRestartAction, 200, false},
		{testRestartAction, 202, false},
	} {
		t.Run(fmt.Sprintf("%s/%d", tc.action, tc.status), func(t *testing.T) {
			var calls atomic.Int32

			client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(tc.status) }))
			run := map[string]func(context.Context, string) error{testStartAction: client.StartContainer, testStopAction: client.StopContainer, testRestartAction: client.RestartContainer}[tc.action]
			err := run(t.Context(), testBusinessContainerName)

			if tc.confirmed {
				require.NoError(t, err)
			} else {
				require.Error(t, err)
			}

			require.Equal(t, int32(1), calls.Load())
		})
	}
}

func TestExactPolicyDeniesBeforeDockerIO(t *testing.T) {
	var calls atomic.Int32

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) { calls.Add(1); w.WriteHeader(http.StatusNoContent) }))

	for _, name := range []string{"0123456789abcdef", "hololive-youtube-collector-a", "hololive-api-extra", "admin-custom", "postgres", "hololive-api/../admin-dashboard", "hololive-api%2f..%2fadmin-dashboard"} {
		require.Error(t, client.StartContainer(t.Context(), name), name)
		require.Error(t, client.RestartContainer(t.Context(), name), name)
		require.Error(t, client.StopContainer(t.Context(), name), name)
	}

	for _, name := range []string{"holo-postgres", "valkey-cache", "deunhealth", testAdminContainerName, "admin-dashboard-ingress", "admin-docker-proxy"} {
		require.Error(t, client.StopContainer(t.Context(), name), name)
	}

	require.Zero(t, calls.Load())
}

func TestDockerActionNeverFollowsRedirect(t *testing.T) {
	var destinationCalls atomic.Int32

	destination := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
		destinationCalls.Add(1)
		w.WriteHeader(http.StatusNoContent)
	}))
	t.Cleanup(destination.Close)

	var calls atomic.Int32

	client := newTestClient(t, http.HandlerFunc(func(w http.ResponseWriter, req *http.Request) {
		calls.Add(1)
		http.Redirect(w, req, destination.URL, http.StatusTemporaryRedirect)
	}))
	require.Error(t, client.RestartContainer(t.Context(), testBusinessContainerName))
	require.Equal(t, int32(1), calls.Load())
	require.Zero(t, destinationCalls.Load())
}
