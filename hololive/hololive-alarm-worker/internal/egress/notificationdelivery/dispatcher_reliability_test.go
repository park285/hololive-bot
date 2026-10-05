package notificationdelivery

import (
	"context"
	"errors"
	"testing"
	"testing/synctest"
	"time"

	"github.com/park285/iris-client-go/v3/iris"
	"github.com/park285/shared-go/v2/pkg/workercontract"
	"github.com/stretchr/testify/require"

	"github.com/kapu/hololive-shared/pkg/domain"
	"github.com/kapu/hololive-shared/pkg/service/sendoutcome"
)

func TestDispatcherQuarantinesUnknownWithoutRetry(t *testing.T) {
	for _, cause := range []error{sendoutcome.ErrHandoffOutcomeUnknown, &iris.HTTPError{StatusCode: 409, Body: `{"code":"CLIENT_REQUEST_ID_OUTCOME_UNKNOWN"}`}} {
		for _, saveError := range []error{nil, errors.New("db unavailable")} {
			failed, quarantined := 0, 0
			repo := &mockDeliveryRepository{
				markFailedFn: func(context.Context, int64, string, int, int, time.Duration, string) (bool, error) {
					failed++
					return true, nil
				},
				markQuarantinedFn: func(ctx context.Context, _ int64, _, _ string) (bool, error) {
					quarantined++

					if ctx.Err() != nil {
						t.Fatal(ctx.Err())
					}

					return saveError == nil, saveError
				},
			}
			d := mustNewDispatcher(t, repo, &mockSender{sendFn: func(context.Context, string, string) error { return cause }}, dispatcherLogger(), nil)
			d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})

			if failed != 0 || quarantined != 1 {
				t.Fatalf("failed=%d quarantined=%d", failed, quarantined)
			}
		}
	}
}

func TestDispatcherAttemptDeadlineAndParentCancellation(t *testing.T) {
	for _, parentDeadline := range []time.Duration{0, 2 * time.Second} {
		t.Run(parentDeadline.String(), func(t *testing.T) {
			synctest.Test(t, func(t *testing.T) {
				ctx := t.Context()

				if parentDeadline > 0 {
					var cancel context.CancelFunc

					ctx, cancel = context.WithTimeout(ctx, parentDeadline)

					defer cancel()
				}

				quarantined := false
				repo := &mockDeliveryRepository{markQuarantinedFn: func(ctx context.Context, _ int64, _, _ string) (bool, error) {
					if ctx.Err() != nil {
						t.Fatal("finalization inherited expired deadline")
					}

					quarantined = true

					return true, nil
				}, markFailedFn: func(context.Context, int64, string, int, int, time.Duration, string) (bool, error) {
					t.Fatal("ambiguous timeout retried")

					return false, nil
				}}

				sender := &mockSender{sendFn: func(ctx context.Context, _, _ string) error {
					<-ctx.Done()

					return ctx.Err()
				}}
				d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
				started := time.Now()

				d.processItem(ctx, &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})

				want := 10 * time.Second

				if parentDeadline > 0 {
					want = parentDeadline
				}

				if elapsed := time.Since(started); elapsed != want || !quarantined {
					t.Fatalf("elapsed=%s want=%s quarantined=%v", elapsed, want, quarantined)
				}
			})
		})
	}
}

func TestDispatcherCanceledBeforeSenderDoesNotSend(t *testing.T) {
	ctx, cancel := context.WithCancel(t.Context())
	cancel()

	d := mustNewDispatcher(t, &mockDeliveryRepository{}, &mockSender{sendFn: func(context.Context, string, string) error {
		t.Fatal("canceled attempt invoked sender")

		return nil
	}}, dispatcherLogger(), nil)
	d.processItem(ctx, &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})
}

func TestDispatcherSuccessfulSendDBFailureRemainsUnretried(t *testing.T) {
	repo := &mockDeliveryRepository{markSentFn: func(context.Context, int64, string) (bool, error) { return false, errors.New("commit response lost") }, markFailedFn: func(context.Context, int64, string, int, int, time.Duration, string) (bool, error) {
		t.Fatal("successful send returned to pending")

		return false, nil
	}}
	d := mustNewDispatcher(t, repo, &mockSender{}, dispatcherLogger(), nil)
	d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})
}

func TestDispatcherRejectsAttemptBudgetExceedingLease(t *testing.T) {
	cfg := testDispatcherConfig()

	cfg.AttemptTimeout = deliveryLease - deliveryFinalizeTimeout

	if _, err := NewDispatcher(&mockDeliveryRepository{}, &mockSender{}, dispatcherLogger(), &cfg); err == nil {
		t.Fatal("unbounded attempt budget accepted")
	}
}

// stale sweep은 lease 만료를 보지 않으므로, 임계값이 lease보다 짧으면 동시에 도는 유지보수가 진행 중인 자기 발송을 격리할 수 있다.
func TestDispatcherStaleSendingThresholdMustCoverSendingLease(t *testing.T) {
	for _, tc := range []struct {
		name      string
		threshold time.Duration
		valid     bool
	}{
		{name: "below lease", threshold: deliveryLease - time.Millisecond, valid: false},
		{name: "attempt plus finalize budget", threshold: 10*time.Second + deliveryFinalizeTimeout, valid: false},
		{name: "equal to lease", threshold: deliveryLease, valid: true},
	} {
		t.Run(tc.name, func(t *testing.T) {
			cfg := testDispatcherConfig()

			cfg.StaleSendingAfter = tc.threshold

			_, err := NewDispatcher(&mockDeliveryRepository{}, &mockSender{}, dispatcherLogger(), &cfg)
			if tc.valid && err != nil {
				t.Fatalf("NewDispatcher() error = %v, want lease-sized threshold accepted", err)
			}

			if !tc.valid && err == nil {
				t.Fatalf("stale sending threshold %s below %s lease accepted", tc.threshold, deliveryLease)
			}
		})
	}
}

func TestGenericProviderAttemptEndsBeforeFinalizationPanic(t *testing.T) {
	tracker := workercontract.NewExecutorTracker()
	totals := &workercontract.Counters{}
	repo := &mockDeliveryRepository{markSentFn: func(context.Context, int64, string) (bool, error) {
		require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
		require.EqualValues(t, 1, totals.Snapshot().Attempts.Success)
		panic("database finalization panic")
	}}
	sender := &mockSender{sendFn: func(context.Context, string, string) error {
		require.EqualValues(t, 1, tracker.Snapshot(time.Now()).InFlight)

		return nil
	}}
	d := mustNewDispatcher(t, repo, sender, dispatcherLogger(), nil)
	d.SetWorkerInstrumentation(tracker, totals)
	require.Panics(t, func() {
		d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})
	})
	require.Equal(t, workercontract.AttemptTotals{Success: 1}, totals.Snapshot().Attempts)
}

func TestGenericProviderPanicClosesAttempt(t *testing.T) {
	tracker := workercontract.NewExecutorTracker()
	totals := &workercontract.Counters{}
	sender := &mockSender{sendFn: func(context.Context, string, string) error { panic("provider panic") }}
	d := mustNewDispatcher(t, &mockDeliveryRepository{}, sender, dispatcherLogger(), nil)
	d.SetWorkerInstrumentation(tracker, totals)
	require.Panics(t, func() {
		d.processItem(t.Context(), &domain.NotificationDeliveryOutbox{ID: 1, Payload: makePayload(t, "hello")})
	})
	require.Zero(t, tracker.Snapshot(time.Now()).InFlight)
	require.Equal(t, workercontract.AttemptTotals{Panic: 1}, totals.Snapshot().Attempts)
}
