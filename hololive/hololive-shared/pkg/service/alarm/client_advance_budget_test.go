package alarm

import (
	"context"
	"net/http"
	"sync/atomic"
	"testing"
	"testing/synctest"
	"time"

	"github.com/stretchr/testify/assert"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
)

type advanceBudgetResult struct {
	result  domain.AdvanceMinutesResult
	err     error
	elapsed time.Duration
}

func startAdvanceBudgetRequest(ctx context.Context, client *Client, minutes int) <-chan advanceBudgetResult {
	done := make(chan advanceBudgetResult, 1)

	go func() {
		started := time.Now()

		result, err := client.UpdateAlarmAdvanceMinutes(ctx, minutes)

		done <- advanceBudgetResult{result: result, err: err, elapsed: time.Since(started)}
	}()

	return done
}

func newAdvanceBudgetClient(timeout time.Duration, sends *atomic.Int32) *Client {
	client := NewClient("http://alarm.test", nil)

	client.httpClient.Timeout = timeout

	client.httpClient.Transport = advanceTransportFunc(func(req *http.Request) (*http.Response, error) {
		sends.Add(1)

		<-req.Context().Done()

		return nil, req.Context().Err()
	})

	client.setTargetMinutes([]int{5, 3, 1})

	return client
}

func TestClientAdvanceConfiguredBudgetIncludesQueueAndTransmission(t *testing.T) {
	for _, timeout := range []time.Duration{10 * time.Second, 40 * time.Millisecond} {
		t.Run(timeout.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				var sends atomic.Int32

				client := newAdvanceBudgetClient(timeout, &sends)

				firstDone := startAdvanceBudgetRequest(t.Context(), client, 7)

				synctest.Wait()

				time.Sleep(timeout / 10)

				secondDone := startAdvanceBudgetRequest(t.Context(), client, 10)

				synctest.Wait()

				time.Sleep(timeout)

				synctest.Wait()

				first, second := <-firstDone, <-secondDone

				require.ErrorIs(t, first.err, context.DeadlineExceeded)

				require.ErrorIs(t, second.err, context.DeadlineExceeded)

				assert.Equal(t, timeout, first.elapsed)

				assert.Equal(t, timeout, second.elapsed, "queue wait must consume the transmission budget")

				assert.Equal(t, domain.ApplyUnknown, second.result.Outcome)

				assert.Equal(t, int32(2), sends.Load())

				assert.Empty(t, client.GetTargetMinutes())
			})
		})
	}
}

func TestClientAdvanceQueuedDeadlineRejectsBeforeSend(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var sends atomic.Int32

		client := newAdvanceBudgetClient(10*time.Second, &sends)

		firstDone := startAdvanceBudgetRequest(t.Context(), client, 7)

		synctest.Wait()

		ctx, cancel := context.WithTimeout(t.Context(), time.Second)

		defer cancel()

		secondDone := startAdvanceBudgetRequest(ctx, client, 10)

		synctest.Wait()

		time.Sleep(time.Second)

		synctest.Wait()

		second := <-secondDone

		require.ErrorIs(t, second.err, context.DeadlineExceeded)

		assert.Equal(t, domain.ApplyRejected, second.result.Outcome)

		assert.Equal(t, time.Second, second.elapsed)

		assert.Equal(t, int32(1), sends.Load())

		assert.Equal(t, []int{5, 3, 1}, client.GetTargetMinutes())

		time.Sleep(9 * time.Second)

		synctest.Wait()

		require.ErrorIs(t, (<-firstDone).err, context.DeadlineExceeded)

		assert.Empty(t, client.GetTargetMinutes())
	})
}
