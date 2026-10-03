package alarm

import (
	"context"
	"errors"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"testing/synctest"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type advanceTransportFunc func(*http.Request) (*http.Response, error)

func (f advanceTransportFunc) RoundTrip(req *http.Request) (*http.Response, error) {
	return f(req)
}

func TestClientAdvanceSerializesRequestsAndCancellationDoesNotSend(t *testing.T) {
	t.Parallel()
	t.Run("response loss", testSerializedAdvanceResponseLoss)
	t.Run("cancel while waiting", testSerializedAdvanceWaitCancellation)
}

func newBlockedAdvanceClient(t *testing.T) (*Client, func(), *atomic.Int32) {
	t.Helper()

	firstRelease := make(chan struct{})
	release := sync.OnceFunc(func() { close(firstRelease) })
	t.Cleanup(release)

	sent := new(atomic.Int32)
	client := NewClient("https://alarm.example", nil)

	client.httpClient.Transport = advanceTransportFunc(func(*http.Request) (*http.Response, error) {
		if sent.Add(1) == 1 {
			<-firstRelease

			return &http.Response{StatusCode: http.StatusOK, Body: io.NopCloser(strings.NewReader(`{"success":true,"data":{"target_minutes":[5,3,1]}}`)), Header: make(http.Header)}, nil
		}

		return nil, io.EOF
	})

	return client, release, sent
}

func testSerializedAdvanceResponseLoss(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		client, release, sent := newBlockedAdvanceClient(t)
		firstDone := make(chan error, 1)

		go func() {
			_, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 5)
			firstDone <- err
		}()

		synctest.Wait()

		secondDone := make(chan error, 1)

		go func() {
			result, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 10)
			assert.Equal(t, domain.ApplyUnknown, result.Outcome)

			secondDone <- err
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), sent.Load(), "second request must wait for the first response")
		release()
		synctest.Wait()
		require.NoError(t, <-firstDone)
		require.ErrorIs(t, <-secondDone, io.EOF)
		assert.Equal(t, int32(2), sent.Load())
		assert.Empty(t, client.GetTargetMinutes())
	})
}

func testSerializedAdvanceWaitCancellation(t *testing.T) {
	t.Parallel()
	synctest.Test(t, func(t *testing.T) {
		client, release, sent := newBlockedAdvanceClient(t)
		firstDone := make(chan error, 1)

		go func() {
			_, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 5)
			firstDone <- err
		}()

		synctest.Wait()

		ctx, cancel := context.WithCancel(t.Context())

		defer cancel()

		secondDone := make(chan error, 1)

		go func() {
			result, err := client.UpdateAlarmAdvanceMinutes(ctx, 10)
			assert.Equal(t, domain.ApplyRejected, result.Outcome)

			secondDone <- err
		}()

		synctest.Wait()
		assert.Equal(t, int32(1), sent.Load())
		cancel()
		synctest.Wait()
		require.ErrorIs(t, <-secondDone, context.Canceled)
		assert.Equal(t, int32(1), sent.Load())
		release()
		synctest.Wait()
		require.NoError(t, <-firstDone)
		assert.Equal(t, []int{5, 3, 1}, client.GetTargetMinutes())
	})
}

func TestClientAdvancePreservesTransportAndCancellationErrors(t *testing.T) {
	t.Parallel()

	transportErr := errors.New("connection reset after write")

	for _, tc := range []struct {
		name          string
		cancelBefore  bool
		cancelInSend  bool
		wantOutcome   domain.ApplyOutcome
		wantTransport int32
	}{
		{name: "transport loss", wantOutcome: domain.ApplyUnknown, wantTransport: 1},
		{name: "cancel before send", cancelBefore: true, wantOutcome: domain.ApplyRejected},
		{name: "cancel after send begins", cancelInSend: true, wantOutcome: domain.ApplyUnknown, wantTransport: 1},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			ctx, cancel := context.WithCancel(t.Context())
			t.Cleanup(cancel)

			var sent atomic.Int32

			client := NewClient("https://alarm.example", nil)
			client.setTargetMinutes([]int{5, 3, 1})

			client.httpClient.Transport = advanceTransportFunc(func(*http.Request) (*http.Response, error) {
				sent.Add(1)

				if tc.cancelInSend {
					cancel()

					return nil, context.Canceled
				}

				return nil, transportErr
			})

			if tc.cancelBefore {
				cancel()
			}

			result, err := client.UpdateAlarmAdvanceMinutes(ctx, 12)
			require.Error(t, err)

			if tc.cancelBefore || tc.cancelInSend {
				require.ErrorIs(t, err, context.Canceled)
			} else {
				require.ErrorIs(t, err, transportErr)
			}

			assert.Equal(t, tc.wantOutcome, result.Outcome)
			assert.Equal(t, 12, result.RequestedMinutes)
			assert.Empty(t, result.TargetMinutes)
			assert.Equal(t, tc.wantTransport, sent.Load())

			if result.Outcome == domain.ApplyUnknown {
				assert.Empty(t, client.GetTargetMinutes())
			} else {
				assert.Equal(t, []int{5, 3, 1}, client.GetTargetMinutes())
			}
		})
	}
}

func TestClientAdvanceRejectsInvalidOriginBeforeSending(t *testing.T) {
	t.Parallel()

	client := NewClient("/relative", nil)

	var sent atomic.Int32

	client.httpClient.Transport = advanceTransportFunc(func(*http.Request) (*http.Response, error) {
		sent.Add(1)

		return nil, io.EOF
	})

	result, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 10)
	require.Error(t, err)
	assert.Equal(t, domain.ApplyRejected, result.Outcome)
	assert.Zero(t, sent.Load())
}

func TestClientAdvanceClassifiesUnconfirmedResponses(t *testing.T) {
	t.Parallel()

	for _, tc := range []struct {
		name   string
		status int
		body   string
	}{
		{name: "server error", status: http.StatusInternalServerError, body: `{"success":false,"error":"worker_failed"}`},
		{name: "proxy input rejection", status: http.StatusBadRequest, body: `{"error":"proxy_bad_request"}`},
		{name: "proxy unauthorized", status: http.StatusUnauthorized, body: `<html>Unauthorized</html>`},
		{name: "unsuccessful envelope", status: http.StatusOK, body: `{"success":false,"error":"worker_failed"}`},
		{name: "missing success", status: http.StatusOK, body: `{"data":{"target_minutes":[10,3,1]}}`},
		{name: "malformed JSON", status: http.StatusOK, body: `{"success":true`},
		{name: "missing data", status: http.StatusOK, body: `{"success":true}`},
		{name: "missing targets", status: http.StatusOK, body: `{"success":true,"data":{}}`},
		{name: "null targets", status: http.StatusOK, body: `{"success":true,"data":{"target_minutes":null}}`},
		{name: "empty targets", status: http.StatusOK, body: `{"success":true,"data":{"target_minutes":[]}}`},
		{name: "invalid targets", status: http.StatusOK, body: `{"success":true,"data":{"target_minutes":[10,0]}}`},
	} {
		t.Run(tc.name, func(t *testing.T) {
			t.Parallel()

			var sent atomic.Int32

			server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, _ *http.Request) {
				sent.Add(1)
				w.WriteHeader(tc.status)

				if _, err := io.Copy(w, strings.NewReader(tc.body)); err != nil {
					t.Errorf("write response: %v", err)
				}
			}))
			t.Cleanup(server.Close)

			client := NewClient(server.URL, nil)
			client.setTargetMinutes([]int{5, 3, 1})

			result, err := client.UpdateAlarmAdvanceMinutes(t.Context(), 10)
			require.Error(t, err)
			assert.Equal(t, domain.ApplyUnknown, result.Outcome)
			assert.Empty(t, result.TargetMinutes)
			assert.Empty(t, client.GetTargetMinutes())
			assert.Equal(t, int32(1), sent.Load())
		})
	}
}
