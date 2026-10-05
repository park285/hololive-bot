package runtime

import (
	"bytes"
	"context"
	"errors"
	"fmt"
	"log/slog"
	"testing"
	"testing/synctest"
	"time"

	"github.com/jackc/pgx/v5/pgconn"
	"github.com/prometheus/client_golang/prometheus/testutil"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-api/internal/youtube/sourceobservation"
	contract "github.com/kapu/hololive-shared/pkg/contracts/sourceobservation"
)

type failureRecordingCase struct {
	name  string
	err   error
	fatal bool
}

func TestFailureRecordingControlsWorkerExit(t *testing.T) {
	for _, operation := range []string{"retry", "dead_letter"} {
		for _, tc := range []failureRecordingCase{
			{name: "slot timeout", err: fmt.Errorf("acquire DB slot: %w", context.DeadlineExceeded)},
			{name: "database contention", err: &pgconn.PgError{Code: "55P03"}},
			{name: "permission denied", err: &pgconn.PgError{Code: "42501"}, fatal: true},
			{name: "invariant", err: errors.New("invalid queue state"), fatal: true},
		} {
			t.Run(operation+"/"+tc.name, func(t *testing.T) {
				testFailureRecordingWorkerExit(t, operation, tc)
			})
		}
	}
}

// testFailureRecordingWorkerExit는 일시 오류로 기록에 실패하면 다음 작업을 계속하고, 그 밖의 실패는 worker를 종료하는지 확인한다.
func testFailureRecordingWorkerExit(t *testing.T, operation string, tc failureRecordingCase) {
	t.Helper()

	cause := context.DeadlineExceeded

	if operation == "dead_letter" {
		cause = errors.New("invalid observation")
	}

	var calls, writes int

	r := newTestRuntime(deadLetterClaimer{
		retry: func(context.Context, sourceobservation.RetryInput) (contract.Status, error) {
			writes++
			return "", tc.err
		},
		deadLetter: func(context.Context, sourceobservation.DeadLetterInput) error {
			writes++
			return tc.err
		},
	}, fakeConsumer{consume: func(context.Context, sourceobservation.Claim) error {
		calls++
		if calls == 1 {
			return cause
		}

		return nil
	}})

	var logs bytes.Buffer

	r.Logger = slog.New(slog.NewTextHandler(&logs, nil))

	counter := youtubeConsumeTotal.WithLabelValues(operation + "_error")
	before := testutil.ToFloat64(counter)
	first := sourceobservation.ClaimWork{ObservationID: 1, LeaseToken: "first"}

	r.workCh <- first

	r.workCh <- sourceobservation.ClaimWork{ObservationID: 2, LeaseToken: "second"}

	close(r.workCh)

	errCh := make(chan error, 1)
	r.runWorker(t.Context(), errCh)

	require.Equal(t, 1, writes)
	require.InDelta(t, before+1, testutil.ToFloat64(counter), 0)

	_, remembered := r.inFlight.Load(first.Key())
	require.False(t, remembered)

	if tc.fatal {
		require.Equal(t, 1, calls)
		require.Len(t, errCh, 1)
		require.ErrorIs(t, <-errCh, tc.err)

		return
	}

	require.Equal(t, 2, calls)
	require.Empty(t, errCh)
	require.Contains(t, logs.String(), "level=ERROR")
	require.Contains(t, logs.String(), "awaiting lease recovery")
	require.Contains(t, logs.String(), tc.err.Error())
}

func TestWorkerContinuesAfterRetryDBSlotTimeout(t *testing.T) {
	synctest.Test(t, func(t *testing.T) {
		var consumed int

		r := newTestRuntime(fakeClaimer{}, fakeConsumer{consume: func(context.Context, sourceobservation.Claim) error {
			consumed++
			return nil
		}})

		r.Config.TransactionTimeout = 10 * time.Millisecond
		r.dbSem = make(chan struct{}, 1)

		r.dbSem <- struct{}{}

		r.workCh <- sourceobservation.ClaimWork{ObservationID: 1}

		r.workCh <- sourceobservation.ClaimWork{ObservationID: 2}

		close(r.workCh)

		errCh := make(chan error, 1)
		counter := youtubeConsumeTotal.WithLabelValues("retry_error")
		before := testutil.ToFloat64(counter)

		go r.runWorker(t.Context(), errCh)

		// 첫 consume와 Retry의 슬롯 대기가 모두 만료된 뒤 다음 작업의 슬롯을 엽니다.
		time.Sleep(2*r.Config.TransactionTimeout + time.Millisecond)
		<-r.dbSem
		synctest.Wait()

		require.Empty(t, errCh)
		require.Equal(t, 1, consumed)
		require.InDelta(t, before+1, testutil.ToFloat64(counter), 0)
	})
}
